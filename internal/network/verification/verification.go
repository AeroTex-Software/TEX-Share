// Package verification probes a configured domain and reports a structured
// status: DNS resolution and target, reachability, HTTPS/certificate validity,
// reverse proxy presence, public UI and (optional) admin UI reachability.
//
// All outbound connections use an SSRF-safe dialer that refuses private,
// loopback and link-local destinations unless explicitly allowed, so an admin
// cannot use the checker to reach internal services.
package verification

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// Options configures a check.
type Options struct {
	Hostname      string   // public hostname to check
	AdminHostname string   // optional admin hostname
	ExpectIPs     []string // expected A/AAAA targets (empty = no target assertion)
	ExpectCNAME   string   // expected CNAME target (optional)
	AllowPrivate  bool     // allow private addresses (LAN setups)
	Timeout       time.Duration
}

// Status is the structured result of a check.
type Status struct {
	Hostname          string   `json:"hostname"`
	DNSResolved       bool     `json:"dnsResolved"`
	Addresses         []string `json:"addresses"`
	CNAME             string   `json:"cname,omitempty"`
	TargetMatches     bool     `json:"targetMatches"`
	Reachable         bool     `json:"reachable"`
	HTTPS             bool     `json:"https"`
	CertValid         bool     `json:"certValid"`
	CertError         string   `json:"certError,omitempty"`
	ProxyDetected     bool     `json:"proxyDetected"`
	ServerHeader      string   `json:"serverHeader,omitempty"`
	PublicUIOK        bool     `json:"publicUiOk"`
	AdminUIReachable  bool     `json:"adminUiReachable"`
	AdminUIHostname   string   `json:"adminUiHostname,omitempty"`
	CheckedAt         int64    `json:"checkedAt"`
	Errors            []string `json:"errors"`
	Fixes             []string `json:"fixes"`
}

// Check runs the full probe set. It never returns an error: failures are
// reported inside the Status so the UI can always render something useful.
func Check(ctx context.Context, opts Options) Status {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	host := strings.ToLower(strings.TrimSpace(opts.Hostname))
	st := Status{Hostname: host, CheckedAt: time.Now().Unix(), Errors: []string{}, Addresses: []string{}}

	resolver := net.DefaultResolver
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		st.Errors = append(st.Errors, "DNS-Auflösung fehlgeschlagen: "+err.Error())
		st.Fixes = append(st.Fixes, "Prüfe, ob der A-/AAAA-Record für "+host+" beim DNS-Anbieter angelegt und propagiert ist.")
	} else {
		st.DNSResolved = true
		st.Addresses = addrs
	}
	if cname, cerr := resolver.LookupCNAME(ctx, host); cerr == nil {
		st.CNAME = strings.TrimSuffix(cname, ".")
	}

	// Target assertion.
	st.TargetMatches = true
	if len(opts.ExpectIPs) > 0 || opts.ExpectCNAME != "" {
		st.TargetMatches = false
		want := map[string]bool{}
		for _, ip := range opts.ExpectIPs {
			want[strings.TrimSpace(ip)] = true
		}
		for _, a := range st.Addresses {
			if want[a] {
				st.TargetMatches = true
			}
		}
		if opts.ExpectCNAME != "" && strings.EqualFold(strings.TrimSuffix(opts.ExpectCNAME, "."), st.CNAME) {
			st.TargetMatches = true
		}
		if !st.TargetMatches {
			st.Errors = append(st.Errors, "Der DNS-Eintrag zeigt nicht auf das erwartete Ziel.")
			st.Fixes = append(st.Fixes, "Setze den A-/AAAA-/CNAME-Record auf die öffentliche Adresse bzw. den Proxy-/Tunnel-Hostnamen.")
		}
	}

	// Public UI probe over HTTPS, then plain HTTP to detect missing TLS.
	httpsOK, tlsOK, certErr, server, proxySeen, err := probe(ctx, host, opts.AllowPrivate)
	st.HTTPS = httpsOK
	st.CertValid = tlsOK
	st.CertError = certErr
	st.ServerHeader = server
	st.ProxyDetected = proxySeen
	if httpsOK {
		st.Reachable = true
		st.PublicUIOK = true
	} else {
		if err != nil {
			st.Errors = append(st.Errors, err.Error())
		}
		httpOK, herr := probeHTTP(ctx, host, opts.AllowPrivate)
		if httpOK {
			st.Reachable = true
			st.PublicUIOK = true
			st.Errors = append(st.Errors, "Die Domain antwortet nur über HTTP; HTTPS/TLS ist nicht aktiv.")
			st.Fixes = append(st.Fixes, "Richte beim Reverse Proxy (oder via Cloudflare) ein TLS-Zertifikat ein.")
		} else if herr != nil {
			st.Errors = append(st.Errors, herr.Error())
		}
	}
	if !st.Reachable && st.DNSResolved {
		st.Fixes = append(st.Fixes, "Stelle sicher, dass Port 3535 erreichbar ist (Portfreigabe, Proxy-Weiterleitung oder Tunnel) und keine Firewall blockt.")
	}

	// Optional admin UI probe.
	if opts.AdminHostname != "" {
		st.AdminUIHostname = strings.ToLower(strings.TrimSpace(opts.AdminHostname))
		if ok, _, _, _, _, _ := probe(ctx, st.AdminUIHostname, opts.AllowPrivate); ok {
			st.AdminUIReachable = true
		}
	}

	if len(st.Errors) == 0 {
		st.Fixes = nil
	}
	return st
}

// probe performs a safe HTTPS GET /healthz and validates the certificate.
func probe(ctx context.Context, host string, allowPrivate bool) (ok, tlsValid bool, certErr, server string, proxySeen bool, err error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = safeDialControl
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/healthz", nil)
	if rerr != nil {
		return false, false, "", "", false, rerr
	}
	resp, derr := client.Do(req)
	if derr != nil {
		// Distinguish a certificate problem from an unreachable host.
		if strings.Contains(strings.ToLower(derr.Error()), "certificate") || strings.Contains(strings.ToLower(derr.Error()), "x509") {
			return false, false, derr.Error(), "", false, fmt.Errorf("TLS-Zertifikat ungültig: %s", derr.Error())
		}
		return false, false, "", "", false, fmt.Errorf("HTTPS-Verbindung fehlgeschlagen: %s", derr.Error())
	}
	defer resp.Body.Close()
	server = resp.Header.Get("Server")
	if resp.Header.Get("Via") != "" || strings.Contains(strings.ToLower(server), "nginx") ||
		strings.Contains(strings.ToLower(server), "caddy") || strings.Contains(strings.ToLower(server), "cloudflare") {
		proxySeen = true
	}
	tlsValid = true
	if resp.TLS != nil {
		if verr := verifyCert(resp.TLS); verr != nil {
			tlsValid = false
			certErr = verr.Error()
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return false, tlsValid, certErr, server, proxySeen, fmt.Errorf("HTTP-Status %d auf /healthz", resp.StatusCode)
	}
	var probeBody struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(body, &probeBody) != nil || probeBody.Service != "texshare" {
		return false, tlsValid, certErr, server, proxySeen, fmt.Errorf("Antwort stammt nicht von einer TEX-Share-Instanz")
	}
	return true, tlsValid, certErr, server, proxySeen, nil
}

func probeHTTP(ctx context.Context, host string, allowPrivate bool) (bool, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = safeDialControl
	}
	client := &http.Client{
		Timeout:   6 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/healthz", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("HTTP-Verbindung fehlgeschlagen: %s", err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP-Status %d auf /healthz", resp.StatusCode)
	}
	var probeBody struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(body, &probeBody) != nil || probeBody.Service != "texshare" {
		return false, fmt.Errorf("Antwort stammt nicht von einer TEX-Share-Instanz")
	}
	return true, nil
}

func verifyCert(state *tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("kein Zertifikat empfangen")
	}
	cert := state.PeerCertificates[0]
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("Zertifikat noch nicht gültig (ab %s)", cert.NotBefore.Format(time.RFC3339))
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("Zertifikat abgelaufen am %s", cert.NotAfter.Format(time.RFC3339))
	}
	if len(state.VerifiedChains) == 0 {
		// Manual verification for callers that disabled system roots.
		pool := x509.NewCertPool()
		for _, c := range state.PeerCertificates[1:] {
			pool.AddCert(c)
		}
		if _, err := cert.Verify(x509.VerifyOptions{DNSName: state.ServerName, Roots: pool, Intermediates: pool}); err != nil {
			return err
		}
	}
	return nil
}

func safeDialControl(network, address string, c syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("unresolvable address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("private address blocked")
	}
	return nil
}
