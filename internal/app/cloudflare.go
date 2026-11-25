package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// cloudflareClient talks to the Cloudflare API with a scoped API token. The
// token is stored encrypted with the installation master key.
type cloudflareClient struct {
	app *App
}

func newCloudflare(a *App) *cloudflareClient { return &cloudflareClient{app: a} }

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	Connections []struct {
		ID         string `json:"id"`
		ColoName   string `json:"colo_name"`
		IsPendingReconnect bool `json:"is_pending_reconnect"`
	} `json:"connections"`
}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

func (c *cloudflareClient) enabled() bool {
	return c.app.cfg.CloudflareOn || c.app.settings.Bool("cloudflare_enabled")
}

func (c *cloudflareClient) apiToken() (string, error) {
	enc := c.app.settings.Get("cf_api_token_enc")
	if enc == "" {
		return "", errors.New("kein Cloudflare API-Token hinterlegt")
	}
	raw, err := c.app.secrets.Open("cloudflare_api", enc)
	if err != nil {
		return "", fmt.Errorf("API-Token konnte nicht entschlüsselt werden: %w", err)
	}
	return string(raw), nil
}

func (c *cloudflareClient) tunnelToken() (string, error) {
	enc := c.app.settings.Get("cf_tunnel_token_enc")
	if enc == "" {
		return "", errors.New("kein Tunnel-Token hinterlegt")
	}
	raw, err := c.app.secrets.Open("cloudflare_tunnel", enc)
	if err != nil {
		return "", fmt.Errorf("Tunnel-Token konnte nicht entschlüsselt werden: %w", err)
	}
	return string(raw), nil
}

func (c *cloudflareClient) call(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.apiToken()
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare API nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env cfEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unerwartete Cloudflare-Antwort (HTTP %d)", resp.StatusCode)
	}
	if !env.Success {
		msg := "Cloudflare API-Fehler"
		if len(env.Errors) > 0 {
			parts := make([]string, 0, len(env.Errors))
			for _, e := range env.Errors {
				parts = append(parts, fmt.Sprintf("%s (%d)", e.Message, e.Code))
			}
			msg = strings.Join(parts, "; ")
		}
		return errors.New(msg)
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return err
		}
	}
	return nil
}

// VerifyToken checks that the stored API token is valid and returns its status.
func (c *cloudflareClient) VerifyToken(ctx context.Context) (map[string]any, error) {
	var res struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodGet, "/user/tokens/verify", nil, &res); err != nil {
		return nil, err
	}
	return map[string]any{"id": res.ID, "status": res.Status}, nil
}

// ListTunnels returns all tunnels of the configured account.
func (c *cloudflareClient) ListTunnels(ctx context.Context) ([]cfTunnel, error) {
	account := c.app.settings.Get("cf_account_id")
	if account == "" {
		return nil, errors.New("Cloudflare Account-ID fehlt")
	}
	var tunnels []cfTunnel
	err := c.call(ctx, http.MethodGet, "/accounts/"+account+"/cfd_tunnel?is_deleted=false", nil, &tunnels)
	return tunnels, err
}

// CreateTunnel creates a named tunnel in the configured account.
func (c *cloudflareClient) CreateTunnel(ctx context.Context, name string) (cfTunnel, error) {
	account := c.app.settings.Get("cf_account_id")
	if account == "" {
		return cfTunnel{}, errors.New("Cloudflare Account-ID fehlt")
	}
	var t cfTunnel
	err := c.call(ctx, http.MethodPost, "/accounts/"+account+"/cfd_tunnel", map[string]any{
		"name": name, "config_src": "cloudflare",
	}, &t)
	return t, err
}

// TunnelToken fetches the connector token for a tunnel.
func (c *cloudflareClient) TunnelToken(ctx context.Context, id string) (string, error) {
	account := c.app.settings.Get("cf_account_id")
	if account == "" {
		return "", errors.New("Cloudflare Account-ID fehlt")
	}
	var token string
	if err := c.call(ctx, http.MethodGet, "/accounts/"+account+"/cfd_tunnel/"+id+"/token", nil, &token); err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("Cloudflare lieferte keinen Tunnel-Token")
	}
	return token, nil
}

// EnsureTunnel reuses the configured tunnel or creates it, then stores the
// connector token encrypted. It returns the tunnel ID.
func (c *cloudflareClient) EnsureTunnel(ctx context.Context) (string, error) {
	name := strings.TrimSpace(c.app.settings.Get("cf_tunnel_name"))
	if name == "" {
		name = "tex-share"
	}
	tunnels, err := c.ListTunnels(ctx)
	if err != nil {
		return "", err
	}
	var existing *cfTunnel
	for i := range tunnels {
		if tunnels[i].Name == name {
			existing = &tunnels[i]
			break
		}
	}
	created := false
	if existing == nil {
		t, err := c.CreateTunnel(ctx, name)
		if err != nil {
			return "", err
		}
		existing = &t
		created = true
	}
	token, err := c.TunnelToken(ctx, existing.ID)
	if err != nil {
		return "", err
	}
	sealed, err := c.app.secrets.Seal("cloudflare_tunnel", []byte(token))
	if err != nil {
		return "", err
	}
	values := map[string]string{
		"cf_tunnel_token_enc": sealed,
		"cf_tunnel_id":        existing.ID,
	}
	if created {
		values["cf_tunnel_managed"] = "1"
	}
	if err := c.app.settings.SetMany(values); err != nil {
		return "", err
	}
	return existing.ID, nil
}

// EnsureDNS points hostname at the tunnel with a proxied CNAME.
func (c *cloudflareClient) EnsureDNS(ctx context.Context, hostname, tunnelID string) error {
	zone := c.app.settings.Get("cf_zone_id")
	if zone == "" {
		return errors.New("Cloudflare Zone-ID fehlt")
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return errors.New("Hostname fehlt")
	}
	target := tunnelID + ".cfargotunnel.com"
	var records []cfDNSRecord
	if err := c.call(ctx, http.MethodGet, "/zones/"+zone+"/dns_records?name="+hostname, nil, &records); err != nil {
		return err
	}
	payload := map[string]any{
		"type": "CNAME", "name": hostname, "content": target, "proxied": true, "ttl": 1,
		"comment": "TEX Share tunnel",
	}
	for _, rec := range records {
		if rec.Type == "CNAME" && strings.EqualFold(rec.Content, target) {
			return nil
		}
		if err := c.call(ctx, http.MethodPut, "/zones/"+zone+"/dns_records/"+rec.ID, payload, nil); err != nil {
			return err
		}
		return nil
	}
	return c.call(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", payload, nil)
}

// RemoveDNS deletes the DNS record for a hostname managed by TEX Share.
func (c *cloudflareClient) RemoveDNS(ctx context.Context, hostname string) error {
	zone := c.app.settings.Get("cf_zone_id")
	if zone == "" {
		return errors.New("Cloudflare Zone-ID fehlt")
	}
	var records []cfDNSRecord
	if err := c.call(ctx, http.MethodGet, "/zones/"+zone+"/dns_records?name="+strings.ToLower(hostname), nil, &records); err != nil {
		return err
	}
	for _, rec := range records {
		if !strings.HasSuffix(rec.Content, ".cfargotunnel.com") {
			continue
		}
		if err := c.call(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+rec.ID, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// Status reports whether the integration is configured and healthy. It never
// leaks the token.
func (c *cloudflareClient) Status() map[string]any {
	configured := c.app.settings.Get("cf_api_token_enc") != "" && c.app.settings.Get("cf_account_id") != ""
	return map[string]any{
		"enabled":    c.enabled(),
		"configured": configured,
		"accountId":  c.app.settings.Get("cf_account_id"),
		"zoneId":     c.app.settings.Get("cf_zone_id"),
		"tunnelName": c.app.settings.Get("cf_tunnel_name"),
		"tunnelId":   c.app.settings.Get("cf_tunnel_id"),
	}
}

// cloudflaredTunnel supervises the cloudflared connector process.
type cloudflaredTunnel struct {
	app *App

	mu        sync.Mutex
	cmd       *exec.Cmd
	proc      *os.Process
	status    string
	message   string
	tokenHash string
	startedAt int64
	restarts  int

	stopCh   chan struct{}
	stopOnce sync.Once
}

func newTunnel(a *App) *cloudflaredTunnel {
	return &cloudflaredTunnel{app: a, status: "stopped", stopCh: make(chan struct{})}
}

func (t *cloudflaredTunnel) enabled() bool {
	return t.app.cfg.CloudflareOn || t.app.settings.Bool("cloudflare_enabled")
}

func (t *cloudflaredTunnel) setStatus(status, msg string) {
	t.mu.Lock()
	t.status = status
	t.message = msg
	t.mu.Unlock()
}

// Start launches the supervisor loop.
func (t *cloudflaredTunnel) Start() { go t.run() }

// Stop terminates cloudflared.
func (t *cloudflaredTunnel) Stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.mu.Lock()
	t.killLocked()
	t.mu.Unlock()
}

func (t *cloudflaredTunnel) run() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			t.mu.Lock()
			t.killLocked()
			t.mu.Unlock()
			return
		default:
		}
		t.tick()
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (t *cloudflaredTunnel) tick() {
	if !t.enabled() {
		t.mu.Lock()
		t.killLocked()
		t.mu.Unlock()
		t.setStatus("disabled", "")
		return
	}
	token, err := t.app.cloudflare.tunnelToken()
	if err != nil {
		t.mu.Lock()
		t.killLocked()
		t.mu.Unlock()
		t.setStatus("unconfigured", err.Error())
		return
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:8])

	t.mu.Lock()
	if t.cmd != nil && t.tokenHash == hash {
		t.mu.Unlock()
		return
	}
	if t.cmd != nil {
		t.killLocked()
	}
	t.mu.Unlock()

	path, err := exec.LookPath(t.app.cfg.Cloudflared)
	if err != nil {
		t.setStatus("unavailable", "cloudflared nicht gefunden: "+err.Error())
		return
	}
	cmd := exec.Command(path, "tunnel", "--no-autoupdate", "--loglevel", "info", "run")
	cmd.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
	setProcAttrs(cmd)
	logDir := filepath.Join(t.app.cfg.DataDir, "cloudflare")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		t.setStatus("error", err.Error())
		return
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "cloudflared.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		t.setStatus("error", err.Error())
		return
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.setStatus("error", "cloudflared Start fehlgeschlagen: "+err.Error())
		return
	}
	t.mu.Lock()
	t.cmd = cmd
	t.proc = cmd.Process
	t.tokenHash = hash
	t.startedAt = util.Now()
	t.restarts++
	t.status = "running"
	t.message = ""
	t.mu.Unlock()
	go func() {
		err := cmd.Wait()
		logFile.Close()
		t.mu.Lock()
		if t.cmd == cmd {
			t.cmd = nil
			t.proc = nil
			t.tokenHash = ""
			if err != nil && t.status == "running" {
				t.status = "error"
				t.message = "cloudflared beendet: " + err.Error()
			}
		}
		t.mu.Unlock()
	}()
}

func (t *cloudflaredTunnel) killLocked() {
	if t.cmd != nil && t.cmd.Process != nil {
		terminateProcess(t.cmd.Process)
	}
	t.cmd = nil
	t.proc = nil
	t.tokenHash = ""
	if t.status == "running" {
		t.status = "stopped"
	}
}

// Status returns connector state for the admin UI.
func (t *cloudflaredTunnel) Status() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	pid := 0
	if t.proc != nil {
		pid = t.proc.Pid
	}
	return map[string]any{
		"enabled":   t.enabled(),
		"status":    t.status,
		"message":   t.message,
		"pid":       pid,
		"startedAt": t.startedAt,
		"restarts":  t.restarts,
	}
}
