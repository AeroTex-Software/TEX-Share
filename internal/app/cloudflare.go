package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

// cloudflareManager drives a locally-managed Cloudflare Tunnel through the
// cloudflared CLI. Authorisation happens with `cloudflared tunnel login`, which
// opens the Cloudflare dashboard in the browser and stores an account
// certificate (cert.pem). No API token is required anywhere in this flow.
//
// Everything lives below <data>/cloudflare so it survives container restarts:
//
//	<data>/cloudflare/home/.cloudflared/cert.pem   account certificate
//	<data>/cloudflare/home/.cloudflared/<id>.json  tunnel credentials
//	<data>/cloudflare/config.yml                   tunnel configuration
type cloudflareManager struct {
	app *App

	mu           sync.Mutex
	loginCmd     *exec.Cmd
	loginURL     string
	loginState   string // idle | waiting | done | error
	loginErr     string
	loginStarted int64
}

func newCloudflare(a *App) *cloudflareManager {
	return &cloudflareManager{app: a, loginState: "idle"}
}

func (m *cloudflareManager) dir() string     { return filepath.Join(m.app.cfg.DataDir, "cloudflare") }
func (m *cloudflareManager) homeDir() string { return filepath.Join(m.dir(), "home") }
func (m *cloudflareManager) cfDir() string   { return filepath.Join(m.homeDir(), ".cloudflared") }
func (m *cloudflareManager) certPath() string {
	return filepath.Join(m.cfDir(), "cert.pem")
}
func (m *cloudflareManager) configPath() string { return filepath.Join(m.dir(), "config.yml") }
func (m *cloudflareManager) credPath(id string) string {
	return filepath.Join(m.cfDir(), id+".json")
}

func (m *cloudflareManager) tunnelID() string { return m.app.settings.Get("cf_tunnel_id") }

func (m *cloudflareManager) tunnelName() string {
	name := strings.TrimSpace(m.app.settings.Get("cf_tunnel_name"))
	if name == "" {
		name = "tex-share"
	}
	return name
}

// connected reports whether an account certificate is present, which means the
// administrator completed the browser login at least once.
func (m *cloudflareManager) connected() bool {
	fi, err := os.Stat(m.certPath())
	return err == nil && fi.Size() > 0
}

func (m *cloudflareManager) enabled() bool {
	return m.app.cfg.CloudflareOn || m.app.settings.Bool("cloudflare_enabled")
}

func (m *cloudflareManager) binary() (string, error) {
	path, err := exec.LookPath(m.app.cfg.Cloudflared)
	if err != nil {
		return "", fmt.Errorf("cloudflared wurde nicht gefunden (%q)", m.app.cfg.Cloudflared)
	}
	return path, nil
}

// env returns the child environment with HOME/USERPROFILE redirected so that
// cloudflared keeps its certificate and credentials inside the data directory.
func (m *cloudflareManager) env() []string {
	overrides := map[string]string{
		"HOME":               m.homeDir(),
		"USERPROFILE":        m.homeDir(),
		"TUNNEL_ORIGIN_CERT": m.certPath(),
	}
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if _, ok := overrides[key]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func (m *cloudflareManager) ensureDirs() error {
	return os.MkdirAll(m.cfDir(), 0o750)
}

func (m *cloudflareManager) command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, err := m.binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = m.env()
	return cmd, nil
}

// run executes a cloudflared subcommand to completion and returns its output.
func (m *cloudflareManager) run(ctx context.Context, args ...string) (string, error) {
	if err := m.ensureDirs(); err != nil {
		return "", err
	}
	cmd, err := m.command(ctx, args...)
	if err != nil {
		return "", err
	}
	setProcAttrs(cmd)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		msg := strings.TrimSpace(text)
		if msg == "" {
			msg = err.Error()
		}
		return text, errors.New(msg)
	}
	return text, nil
}

/* -------------------------------------------------------------------------- */
/* Browser login                                                              */
/* -------------------------------------------------------------------------- */

var cfLoginURLRe = regexp.MustCompile(`https://dash\.cloudflare\.com/\S+`)

// LoginState reports the progress of the current/last login attempt.
func (m *cloudflareManager) LoginState() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{"state": m.loginState, "url": m.loginURL, "error": m.loginErr}
}

// StartLogin launches `cloudflared tunnel login` and returns the dashboard URL
// the administrator must open. The process keeps running in the background
// until Cloudflare redirects the browser back to it.
func (m *cloudflareManager) StartLogin() (string, error) {
	m.mu.Lock()
	if m.loginCmd != nil {
		url := m.loginURL
		m.mu.Unlock()
		if url != "" {
			return url, nil
		}
		return "", errors.New("eine Anmeldung läuft bereits")
	}
	m.loginState = "waiting"
	m.loginURL = ""
	m.loginErr = ""
	m.loginStarted = util.Now()
	m.mu.Unlock()

	if err := m.ensureDirs(); err != nil {
		m.failLogin(err)
		return "", err
	}
	cmd, err := m.command(context.Background(), "tunnel", "login")
	if err != nil {
		m.failLogin(err)
		return "", err
	}
	setProcAttrs(cmd)
	pr, pw, err := os.Pipe()
	if err != nil {
		m.failLogin(err)
		return "", err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		m.failLogin(err)
		return "", err
	}
	pw.Close()
	m.mu.Lock()
	m.loginCmd = cmd
	m.mu.Unlock()
	go m.consumeLogin(pr, cmd)

	// Wait briefly for the URL so the UI can show it immediately.
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		url, state, errMsg := m.loginURL, m.loginState, m.loginErr
		m.mu.Unlock()
		switch {
		case url != "":
			return url, nil
		case state == "error":
			return "", errors.New(errMsg)
		case state == "done":
			return "", nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", nil
}

func (m *cloudflareManager) consumeLogin(r io.ReadCloser, cmd *exec.Cmd) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var tail []string
	for scanner.Scan() {
		line := scanner.Text()
		tail = append(tail, line)
		if len(tail) > 12 {
			tail = tail[1:]
		}
		if m.loginURL == "" {
			if u := cfLoginURLRe.FindString(line); u != "" {
				m.mu.Lock()
				m.loginURL = u
				m.mu.Unlock()
			}
		}
	}
	waitErr := cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.loginCmd = nil
	if m.loginState != "waiting" {
		return // cancelled
	}
	if waitErr == nil && m.connected() {
		m.loginState = "done"
		m.loginErr = ""
		return
	}
	m.loginState = "error"
	msg := strings.TrimSpace(strings.Join(tail, " "))
	if msg == "" && waitErr != nil {
		msg = waitErr.Error()
	}
	if msg == "" {
		msg = "Anmeldung fehlgeschlagen"
	}
	m.loginErr = msg
}

func (m *cloudflareManager) failLogin(err error) {
	m.mu.Lock()
	m.loginState = "error"
	m.loginErr = err.Error()
	m.mu.Unlock()
}

// CancelLogin aborts a pending login.
func (m *cloudflareManager) CancelLogin() {
	m.mu.Lock()
	cmd := m.loginCmd
	m.loginCmd = nil
	m.loginState = "idle"
	m.loginURL = ""
	m.loginErr = ""
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		terminateProcess(cmd.Process)
	}
}

/* -------------------------------------------------------------------------- */
/* Tunnel management                                                          */
/* -------------------------------------------------------------------------- */

type cfCredentials struct {
	AccountTag   string `json:"AccountTag"`
	TunnelSecret string `json:"TunnelSecret"`
	TunnelID     string `json:"TunnelID"`
	TunnelName   string `json:"TunnelName"`
}

var cfTunnelIDRe = regexp.MustCompile(`with id ([0-9a-fA-F-]{36})`)

// findCredentials locates a tunnel credentials file, optionally matching name.
func (m *cloudflareManager) findCredentials(name string) (cfCredentials, string, bool) {
	entries, err := os.ReadDir(m.cfDir())
	if err != nil {
		return cfCredentials{}, "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(m.cfDir(), e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var c cfCredentials
		if json.Unmarshal(raw, &c) != nil || c.TunnelID == "" {
			continue
		}
		if name == "" || c.TunnelName == name {
			return c, path, true
		}
	}
	return cfCredentials{}, "", false
}

// EnsureTunnel reuses an existing tunnel/credentials file or creates a new
// locally-managed tunnel through cloudflared.
func (m *cloudflareManager) EnsureTunnel(ctx context.Context) (cfCredentials, error) {
	if !m.connected() {
		return cfCredentials{}, errors.New("nicht mit Cloudflare verbunden")
	}
	name := m.tunnelName()
	if c, _, ok := m.findCredentials(name); ok {
		_ = m.app.settings.SetMany(map[string]string{"cf_tunnel_id": c.TunnelID, "cf_tunnel_name": c.TunnelName})
		return c, nil
	}
	out, err := m.run(ctx, "tunnel", "create", name)
	if err != nil {
		// A tunnel with this name may already exist remotely.
		if c, _, ok := m.findCredentials(name); ok {
			_ = m.app.settings.SetMany(map[string]string{"cf_tunnel_id": c.TunnelID, "cf_tunnel_name": c.TunnelName})
			return c, nil
		}
		return cfCredentials{}, err
	}
	c, _, ok := m.findCredentials(name)
	if !ok {
		if mm := cfTunnelIDRe.FindStringSubmatch(out); len(mm) == 2 {
			return cfCredentials{}, fmt.Errorf("Tunnel %s erstellt, aber Zugangsdatei fehlt", mm[1])
		}
		return cfCredentials{}, fmt.Errorf("Zugangsdatei nicht gefunden: %s", strings.TrimSpace(out))
	}
	_ = m.app.settings.SetMany(map[string]string{"cf_tunnel_id": c.TunnelID, "cf_tunnel_name": c.TunnelName})
	return c, nil
}

// RouteDNS creates the proxied CNAME that points a hostname at the tunnel.
func (m *cloudflareManager) RouteDNS(ctx context.Context, name, hostname string) error {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return nil
	}
	out, err := m.run(ctx, "tunnel", "route", "dns", name, hostname)
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "already exists") {
			return nil
		}
		return err
	}
	return nil
}

// publicPort extracts the port the public UI listens on inside the container.
func (m *cloudflareManager) publicPort() string {
	if _, port, err := net.SplitHostPort(m.app.cfg.PublicAddr); err == nil && port != "" {
		return port
	}
	return "3535"
}

// WriteConfig writes the cloudflared configuration file for a tunnel.
func (m *cloudflareManager) WriteConfig(c cfCredentials, hostname string) error {
	if err := m.ensureDirs(); err != nil {
		return err
	}
	service := "http://localhost:" + m.publicPort()
	cred := strings.ReplaceAll(m.credPath(c.TunnelID), `\`, `\\`)
	var b strings.Builder
	fmt.Fprintf(&b, "tunnel: %s\n", c.TunnelID)
	fmt.Fprintf(&b, "credentials-file: \"%s\"\n", cred)
	b.WriteString("ingress:\n")
	if hostname != "" {
		fmt.Fprintf(&b, "  - hostname: %s\n    service: %s\n", hostname, service)
	}
	fmt.Fprintf(&b, "  - service: %s\n", service)
	return os.WriteFile(m.configPath(), []byte(b.String()), 0o600)
}

// RegenerateConfig rebuilds the config file from the stored credentials.
func (m *cloudflareManager) RegenerateConfig() error {
	c, _, ok := m.findCredentials(m.tunnelName())
	if !ok {
		return errors.New("keine Tunnel-Zugangsdaten gefunden")
	}
	return m.WriteConfig(c, m.app.settings.Get("cf_hostname"))
}

// Disconnect deletes the local certificate, credentials and configuration and
// forgets the tunnel. The remote tunnel is removed on a best-effort basis.
func (m *cloudflareManager) Disconnect(ctx context.Context) error {
	m.CancelLogin()
	if id := m.tunnelID(); id != "" && m.connected() {
		_, _ = m.run(ctx, "tunnel", "delete", "-f", id)
	}
	_ = os.Remove(m.certPath())
	_ = os.Remove(m.configPath())
	if entries, err := os.ReadDir(m.cfDir()); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				_ = os.Remove(filepath.Join(m.cfDir(), e.Name()))
			}
		}
	}
	return m.app.settings.SetMany(map[string]string{
		"cloudflare_enabled": "0",
		"cf_tunnel_id":       "",
		"cf_hostname":        "",
	})
}

// Status reports the integration state without leaking any secret material.
func (m *cloudflareManager) Status() map[string]any {
	m.mu.Lock()
	loginState, loginURL, loginErr := m.loginState, m.loginURL, m.loginErr
	m.mu.Unlock()
	_, binErr := m.binary()
	available := binErr == nil
	availMsg := ""
	if binErr != nil {
		availMsg = binErr.Error()
	}
	return map[string]any{
		"connected":      m.connected(),
		"enabled":        m.enabled(),
		"available":      available,
		"availableError": availMsg,
		"loginState":     loginState,
		"loginUrl":       loginURL,
		"loginError":     loginErr,
		"tunnelId":       m.tunnelID(),
		"tunnelName":     m.tunnelName(),
		"hostname":       m.app.settings.Get("cf_hostname"),
	}
}

/* -------------------------------------------------------------------------- */
/* Connector supervisor                                                       */
/* -------------------------------------------------------------------------- */

// cloudflaredTunnel supervises the cloudflared connector process.
type cloudflaredTunnel struct {
	app *App

	mu         sync.Mutex
	cmd        *exec.Cmd
	proc       *os.Process
	status     string
	message    string
	configHash string
	startedAt  int64
	restarts   int

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
	mgr := t.app.cloudflare
	if !t.enabled() {
		t.mu.Lock()
		t.killLocked()
		t.mu.Unlock()
		t.setStatus("disabled", "")
		return
	}
	if mgr == nil || !mgr.connected() {
		t.mu.Lock()
		t.killLocked()
		t.mu.Unlock()
		t.setStatus("unconfigured", "Nicht mit Cloudflare verbunden")
		return
	}
	id := mgr.tunnelID()
	if id == "" {
		t.mu.Lock()
		t.killLocked()
		t.mu.Unlock()
		t.setStatus("unconfigured", "Kein Tunnel eingerichtet")
		return
	}
	if _, err := os.Stat(mgr.configPath()); err != nil {
		if err := mgr.RegenerateConfig(); err != nil {
			t.setStatus("unconfigured", err.Error())
			return
		}
	}
	raw, _ := os.ReadFile(mgr.configPath())
	sum := sha256.Sum256(append(raw, []byte(id)...))
	hash := hex.EncodeToString(sum[:8])

	t.mu.Lock()
	if t.cmd != nil && t.configHash == hash {
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
	cmd := exec.Command(path, "tunnel", "--config", mgr.configPath(), "--no-autoupdate", "--loglevel", "info", "run", id)
	cmd.Env = mgr.env()
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
	t.configHash = hash
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
			t.configHash = ""
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
	t.configHash = ""
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
