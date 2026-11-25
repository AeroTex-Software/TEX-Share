// Package ddns implements a provider-agnostic dynamic DNS engine.
//
// It detects the public IPv4/IPv6 address, notices changes, and delegates the
// actual record update to a caller-supplied function. That keeps the engine
// independent of any specific DNS provider (Cloudflare today, others later) and
// makes it possible to leave DDNS entirely manual when the DNS host has no API.
//
// Important: DDNS only keeps a record in sync with a changing public address.
// It cannot make a host reachable behind CGNAT or DS-Lite; use a tunnel or a
// different network path in that case.
package ddns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is the persistence interface (satisfied by settings.Store).
type State interface {
	Get(key string) string
	Set(key, value string) error
}

// Config controls one engine tick.
type Config struct {
	Enabled  bool
	Interval time.Duration
	IPv4     bool
	IPv6     bool
}

// ApplyFunc updates the DNS records for the given addresses. It is only called
// when an address actually changed.
type ApplyFunc func(ctx context.Context, v4, v6 string) error

// Engine detects public addresses and drives change-only updates.
type Engine struct {
	state State
	log   *slog.Logger

	mu      sync.Mutex
	lastRun time.Time
	running bool
}

// New returns an engine backed by state.
func New(state State, log *slog.Logger) *Engine {
	return &Engine{state: state, log: log}
}

// Tick runs the engine if it is enabled and the interval has elapsed. It is
// safe to call every minute from the background scheduler.
func (e *Engine) Tick(ctx context.Context, apply ApplyFunc, cfg Config) {
	if !cfg.Enabled {
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	e.mu.Lock()
	if e.running || time.Since(e.lastRun) < interval {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.running = false
		e.lastRun = time.Now()
		e.mu.Unlock()
	}()
	if err := e.RunOnce(ctx, apply, cfg); err != nil {
		if e.log != nil {
			e.log.Warn("ddns run failed", "error", err.Error())
		}
	}
}

// RunOnce performs a single detect/compare/apply cycle. It is exported so the
// admin can trigger an update on demand.
func (e *Engine) RunOnce(ctx context.Context, apply ApplyFunc, cfg Config) error {
	var v4, v6 string
	var v4Err, v6Err error
	if cfg.IPv4 {
		v4, v4Err = DetectPublicIPv4(ctx)
	}
	if cfg.IPv6 {
		v6, v6Err = DetectPublicIPv6(ctx)
	}
	_ = e.state.Set("ddns_last_check", strconv.FormatInt(time.Now().Unix(), 10))
	if v4 != "" {
		_ = e.state.Set("public_ipv4", v4)
	}
	if v6 != "" {
		_ = e.state.Set("public_ipv6", v6)
	}

	last4 := e.state.Get("ddns_last_ipv4")
	last6 := e.state.Get("ddns_last_ipv6")
	changed4 := cfg.IPv4 && v4 != "" && v4 != last4
	changed6 := cfg.IPv6 && v6 != "" && v6 != last6

	if v4Err != nil && v6Err != nil {
		msg := "IP-Ermittlung fehlgeschlagen"
		if v4Err != nil {
			msg += ": " + v4Err.Error()
		} else if v6Err != nil {
			msg += ": " + v6Err.Error()
		}
		_ = e.state.Set("ddns_last_error", msg)
		e.bumpRetries()
		return fmt.Errorf("%s", msg)
	}
	if !changed4 && !changed6 {
		// Nothing to do; clear a stale error so the UI recovers.
		if e.state.Get("ddns_last_error") != "" {
			_ = e.state.Set("ddns_last_error", "")
			_ = e.state.Set("ddns_retries", "0")
		}
		return nil
	}

	if apply == nil {
		return nil
	}
	if err := apply(ctx, v4, v6); err != nil {
		_ = e.state.Set("ddns_last_error", err.Error())
		e.bumpRetries()
		return err
	}
	if changed4 {
		_ = e.state.Set("ddns_last_ipv4", v4)
	}
	if changed6 {
		_ = e.state.Set("ddns_last_ipv6", v6)
	}
	_ = e.state.Set("ddns_last_update", strconv.FormatInt(time.Now().Unix(), 10))
	_ = e.state.Set("ddns_last_error", "")
	_ = e.state.Set("ddns_retries", "0")
	if e.log != nil {
		e.log.Info("ddns updated", "ipv4", v4, "ipv6", v6)
	}
	return nil
}

func (e *Engine) bumpRetries() {
	n, _ := strconv.Atoi(e.state.Get("ddns_retries"))
	_ = e.state.Set("ddns_retries", strconv.Itoa(n+1))
}

// Status reports the engine state for the admin UI.
func (e *Engine) Status() map[string]any {
	parse := func(k string) int64 {
		v, _ := strconv.ParseInt(e.state.Get(k), 10, 64)
		return v
	}
	return map[string]any{
		"enabled":     isTrue(e.state.Get("ddns_enabled")),
		"provider":    e.state.Get("ddns_provider"),
		"ipv4Enabled": isTrue(e.state.Get("ddns_ipv4")),
		"ipv6Enabled": isTrue(e.state.Get("ddns_ipv6")),
		"publicIPv4":  e.state.Get("public_ipv4"),
		"publicIPv6":  e.state.Get("public_ipv6"),
		"lastIPv4":    e.state.Get("ddns_last_ipv4"),
		"lastIPv6":    e.state.Get("ddns_last_ipv6"),
		"lastCheck":   parse("ddns_last_check"),
		"lastUpdate":  parse("ddns_last_update"),
		"lastError":   e.state.Get("ddns_last_error"),
		"retries":     parse("ddns_retries"),
	}
}

func isTrue(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

/* -------------------------------------------------------------------------- */
/* Public IP detection                                                        */
/* -------------------------------------------------------------------------- */

var ipv4Endpoints = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://1.1.1.1/cdn-cgi/trace",
}

var ipv6Endpoints = []string{
	"https://api6.ipify.org",
	"https://ipv6.icanhazip.com",
	"https://[2606:4700:4700::1111]/cdn-cgi/trace",
}

// DetectPublicIPv4 returns the public IPv4 address, trying several services.
func DetectPublicIPv4(ctx context.Context) (string, error) {
	return detect(ctx, ipv4Endpoints, false)
}

// DetectPublicIPv6 returns the public IPv6 address, trying several services.
func DetectPublicIPv6(ctx context.Context) (string, error) {
	return detect(ctx, ipv6Endpoints, true)
}

func detect(ctx context.Context, endpoints []string, want6 bool) (string, error) {
	client := &http.Client{Timeout: 6 * time.Second}
	var lastErr error
	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "texshare-ddns")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", ep, resp.StatusCode)
			continue
		}
		ip := parseIPBody(string(body), want6)
		if ip != "" {
			return ip, nil
		}
		lastErr = fmt.Errorf("%s: keine gültige Adresse", ep)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("keine IP-Ermittlung möglich")
	}
	return "", lastErr
}

// parseIPBody extracts an IP from a plain-text or cloudflare /cdn-cgi/trace body.
func parseIPBody(body string, want6 bool) string {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "fl=") || strings.Contains(body, "\nip=") {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "ip=") {
				if ip := net.ParseIP(strings.TrimSpace(strings.TrimPrefix(line, "ip="))); ip != nil {
					if (ip.To4() == nil) == want6 {
						return ip.String()
					}
				}
			}
		}
		return ""
	}
	if ip := net.ParseIP(body); ip != nil {
		if (ip.To4() == nil) == want6 {
			return ip.String()
		}
	}
	return ""
}
