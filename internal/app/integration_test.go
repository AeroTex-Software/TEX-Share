package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AeroTex-Software/TEX-Share/internal/config"
)

const (
	testAdminUser = "admin"
	testAdminPass = "Sup3rSecret!Pass"
)

type testEnv struct {
	t      *testing.T
	app    *App
	public *httptest.Server
	admin  *httptest.Server
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"", "db", "files", "uploads", "quarantine", "backups", "branding", "clamav", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	cfg := &config.Config{
		DataDir:     dir,
		PublicAddr:  "127.0.0.1:0",
		AdminAddr:   "127.0.0.1:0",
		ClamavMode:  "off",
		ClamavPath:  "clamd",
		Cloudflared: "cloudflared",
		Dev:         true,
		Version:     "test",
		Commit:      "test",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	env := &testEnv{t: t, app: app}
	env.public = httptest.NewServer(app.PublicHandler())
	env.admin = httptest.NewServer(app.AdminHandler())
	t.Cleanup(func() {
		env.public.Close()
		env.admin.Close()
		app.Close()
	})
	return env
}

type client struct {
	t   *testing.T
	hc  *http.Client
	csr string
}

func newClient(t *testing.T) *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &client{t: t, hc: &http.Client{Jar: jar}}
}

// do performs a request. body may be nil, a []byte, or any JSON-serialisable
// value (which is encoded and sent as application/json).
func (c *client) do(method, url string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var reader io.Reader
	contentType := ""
	switch v := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(v)
		contentType = "application/octet-stream"
	case string:
		reader = strings.NewReader(v)
		contentType = "application/json"
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
		contentType = "application/json"
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.csr != "" {
		req.Header.Set("X-CSRF-Token", c.csr)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func (c *client) json(method, url string, body any, out any) int {
	c.t.Helper()
	resp, data := c.do(method, url, body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("decode %s %s: %v (body=%s)", method, url, err, data)
		}
	}
	return resp.StatusCode
}

// setupAdmin runs the first-run wizard against the admin server and stores the
// returned CSRF token on the client.
func (c *client) setupAdmin(base string) {
	c.t.Helper()
	var res struct {
		OK   bool `json:"ok"`
		CSRF string
	}
	code := c.json("POST", base+"/api/admin/setup", map[string]any{
		"username": testAdminUser, "displayName": "Admin", "password": testAdminPass,
		"appName": "TEX Share Test", "domain": "localhost",
	}, &res)
	if code != http.StatusCreated {
		c.t.Fatalf("setup status=%d", code)
	}
	c.csr = res.CSRF
}

func (c *client) login(base, realm string) {
	c.t.Helper()
	var res struct {
		OK   bool `json:"ok"`
		CSRF string
	}
	code := c.json("POST", base+"/api/"+realm+"/auth/login", map[string]any{
		"username": testAdminUser, "password": testAdminPass, "remember": false,
	}, &res)
	if code != http.StatusOK || !res.OK {
		c.t.Fatalf("login status=%d ok=%v", code, res.OK)
	}
	c.csr = res.CSRF
}

func TestSetupLoginAndRealmSeparation(t *testing.T) {
	env := newTestEnv(t)

	// Before setup the admin API advertises that setup is required.
	anon := newClient(t)
	var status struct {
		NeedsSetup bool `json:"needsSetup"`
	}
	if code := anon.json("GET", env.admin.URL+"/api/admin/setup/status", nil, &status); code != http.StatusOK || !status.NeedsSetup {
		t.Fatalf("setup status code=%d needsSetup=%v", code, status.NeedsSetup)
	}

	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	// The public API rejects the admin session (separate realms/cookies).
	var me struct {
		Realm string `json:"realm"`
	}
	if code := admin.json("GET", env.public.URL+"/api/public/auth/me", nil, &me); code != http.StatusUnauthorized {
		t.Fatalf("public me with admin session code=%d (want 401)", code)
	}

	// Admin me works.
	if code := admin.json("GET", env.admin.URL+"/api/admin/auth/me", nil, &me); code != http.StatusOK || me.Realm != "admin" {
		t.Fatalf("admin me code=%d realm=%q", code, me.Realm)
	}

	// A second setup attempt is refused.
	var dup map[string]any
	if code := admin.json("POST", env.admin.URL+"/api/admin/setup", map[string]any{
		"username": "other", "password": testAdminPass, "domain": "localhost",
	}, &dup); code != http.StatusForbidden {
		t.Fatalf("second setup code=%d (want 403)", code)
	}
}

func TestCSRFRequiredForMutations(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	// A mutating request without the CSRF header must fail.
	noCSRF := newClient(t)
	noCSRF.csr = ""
	// Share the session cookies but not the token.
	noCSRF.hc = admin.hc
	var out map[string]any
	code := noCSRF.json("PATCH", env.admin.URL+"/api/admin/settings", map[string]any{
		"settings": map[string]string{"app_name": "Hacked"},
	}, &out)
	if code != http.StatusForbidden {
		t.Fatalf("PATCH without CSRF code=%d (want 403)", code)
	}

	// With the token it succeeds.
	code = admin.json("PATCH", env.admin.URL+"/api/admin/settings", map[string]any{
		"settings": map[string]string{"app_name": "Renamed"},
	}, &out)
	if code != http.StatusOK {
		t.Fatalf("PATCH with CSRF code=%d (want 200)", code)
	}
}

func TestTextShareLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	// The admin can also use the public UI with a public session.
	pub := newClient(t)
	pub.login(env.public.URL, "public")

	var cfg struct {
		Domains []struct {
			ID int64 `json:"id"`
		} `json:"domains"`
	}
	if code := pub.json("GET", env.public.URL+"/api/public/config", nil, &cfg); code != http.StatusOK || len(cfg.Domains) == 0 {
		t.Fatalf("config code=%d domains=%d", code, len(cfg.Domains))
	}

	var created struct {
		Share struct {
			ID      string `json:"id"`
			Token   string `json:"token"`
			URLPath string `json:"urlPath"`
		} `json:"share"`
	}
	code := pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
		"type": "text", "domainId": cfg.Domains[0].ID, "slug": "hello-world",
		"title": "Hello", "description": "desc", "textContent": "Hello TEX Share",
		"expiresAt": 0, "downloadLimit": 0,
	}, &created)
	if code != http.StatusCreated || created.Share.Token == "" {
		t.Fatalf("create share code=%d token=%q", code, created.Share.Token)
	}

	// Anonymous access sees metadata but no content until /view.
	anon := newClient(t)
	var view struct {
		Available        bool   `json:"available"`
		Type             string `json:"type"`
		Title            string `json:"title"`
		TextContent      string `json:"textContent"`
		AccessGranted    bool   `json:"accessGranted"`
		RequiresPassword bool   `json:"requiresPassword"`
	}
	if code := anon.json("GET", env.public.URL+"/api/public/share/"+created.Share.Token, nil, &view); code != http.StatusOK {
		t.Fatalf("share view code=%d", code)
	}
	if !view.Available || view.Type != "text" || view.Title != "Hello" {
		t.Fatalf("unexpected share view: %+v", view)
	}
	if view.RequiresPassword {
		t.Fatalf("text share unexpectedly requires a password")
	}
	if resp, _ := anon.do("POST", env.public.URL+"/api/public/share/"+created.Share.Token+"/view", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("view count code=%d (want 204)", resp.StatusCode)
	}
	if code := anon.json("GET", env.public.URL+"/api/public/share/"+created.Share.Token, nil, &view); code != http.StatusOK || view.TextContent != "Hello TEX Share" {
		t.Fatalf("post-view share content code=%d text=%q", code, view.TextContent)
	}

	// Unknown tokens are not found.
	if code := anon.json("GET", env.public.URL+"/api/public/share/doesnotexist", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown token code=%d (want 404)", code)
	}
}

func TestPasswordProtectedShare(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)
	pub := newClient(t)
	pub.login(env.public.URL, "public")

	var cfg struct {
		Domains []struct {
			ID int64 `json:"id"`
		} `json:"domains"`
	}
	pub.json("GET", env.public.URL+"/api/public/config", nil, &cfg)

	var created struct {
		Share struct {
			Token string `json:"token"`
		} `json:"share"`
	}
	code := pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
		"type": "text", "domainId": cfg.Domains[0].ID, "slug": "locked",
		"title": "Locked", "textContent": "top secret", "password": "hunter2pass",
		"expiresAt": 0, "downloadLimit": 0,
	}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create password share code=%d", code)
	}
	tok := created.Share.Token

	anon := newClient(t)
	var view struct {
		RequiresPassword bool `json:"requiresPassword"`
		AccessGranted    bool `json:"accessGranted"`
		TextContent      string `json:"textContent"`
	}
	anon.json("GET", env.public.URL+"/api/public/share/"+tok, nil, &view)
	if !view.RequiresPassword || view.AccessGranted {
		t.Fatalf("expected locked share, got %+v", view)
	}

	// Wrong password is rejected.
	if code := anon.json("POST", env.public.URL+"/api/public/share/"+tok+"/unlock", map[string]any{"password": "nope"}, nil); code != http.StatusForbidden {
		t.Fatalf("wrong password code=%d (want 403)", code)
	}

	// Correct password grants access for the session.
	var unlocked struct {
		OK            bool   `json:"ok"`
		AccessGranted bool   `json:"accessGranted"`
		TextContent   string `json:"textContent"`
	}
	if code := anon.json("POST", env.public.URL+"/api/public/share/"+tok+"/unlock", map[string]any{"password": "hunter2pass"}, &unlocked); code != http.StatusOK || !unlocked.AccessGranted {
		t.Fatalf("unlock code=%d granted=%v", code, unlocked.AccessGranted)
	}
	if unlocked.TextContent != "top secret" {
		t.Fatalf("unlocked text=%q", unlocked.TextContent)
	}
}

func TestUploadDownloadAndLimit(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)
	pub := newClient(t)
	pub.login(env.public.URL, "public")

	var cfg struct {
		Domains []struct {
			ID int64 `json:"id"`
		} `json:"domains"`
	}
	pub.json("GET", env.public.URL+"/api/public/config", nil, &cfg)

	content := []byte("TEX Share integration test payload")

	var initRes struct {
		ID     string `json:"id"`
		Mode   string `json:"mode"`
		Chunks int    `json:"totalChunks"`
	}
	code := pub.json("POST", env.public.URL+"/api/public/uploads", map[string]any{
		"filename": "payload.txt", "size": len(content), "mime": "text/plain",
	}, &initRes)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("upload init code=%d", code)
	}
	if initRes.Mode != "direct" {
		t.Fatalf("small upload mode=%q (want direct)", initRes.Mode)
	}

	// Direct upload of the raw bytes.
	if resp, _ := pub.do("PUT", env.public.URL+"/api/public/uploads/"+initRes.ID, content); resp.StatusCode != http.StatusOK {
		t.Fatalf("direct upload code=%d", resp.StatusCode)
	}
	var comp struct {
		OK         bool   `json:"ok"`
		ScanStatus string `json:"scanStatus"`
	}
	if code := pub.json("POST", env.public.URL+"/api/public/uploads/"+initRes.ID+"/complete", map[string]any{}, &comp); code != http.StatusOK || !comp.OK {
		t.Fatalf("upload complete code=%d ok=%v", code, comp.OK)
	}

	// Create a file share with a download limit of one.
	var created struct {
		Share struct {
			Token string `json:"token"`
		} `json:"share"`
	}
	code = pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
		"type": "file", "domainId": cfg.Domains[0].ID, "slug": "payload",
		"title": "Payload", "uploadIds": []string{initRes.ID},
		"expiresAt": 0, "downloadLimit": 1,
	}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create file share code=%d", code)
	}
	tok := created.Share.Token

	anon := newClient(t)
	base := env.public.URL + "/api/public/share/" + tok + "/files/" + initRes.ID

	resp, data := anon.do("GET", base+"/download", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download code=%d", resp.StatusCode)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("download body mismatch: %q", data)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("Content-Disposition=%q", cd)
	}

	// A second download exceeds the limit.
	if resp, _ := anon.do("GET", base+"/download", nil); resp.StatusCode == http.StatusOK {
		t.Fatalf("second download unexpectedly succeeded")
	}

	// A fresh share cannot reuse an already-attached upload.
	var created2 map[string]any
	if code := pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
		"type": "file", "domainId": cfg.Domains[0].ID, "slug": "payload2",
		"title": "Payload2", "uploadIds": []string{initRes.ID},
		"expiresAt": 0, "downloadLimit": 0,
	}, &created2); code == http.StatusCreated {
		t.Fatalf("reusing a consumed upload unexpectedly succeeded")
	}
}

func TestAuditChainVerifies(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	// Perform a few auditable actions.
	pub := newClient(t)
	pub.login(env.public.URL, "public")
	var cfg struct {
		Domains []struct {
			ID int64 `json:"id"`
		} `json:"domains"`
	}
	pub.json("GET", env.public.URL+"/api/public/config", nil, &cfg)
	for i, slug := range []string{"a", "b", "c"} {
		var out map[string]any
		if code := pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
			"type": "text", "domainId": cfg.Domains[0].ID, "slug": slug,
			"title": slug, "textContent": slug, "expiresAt": 0, "downloadLimit": 0,
		}, &out); code != http.StatusCreated {
			t.Fatalf("create share %d code=%d", i, code)
		}
	}

	var verify struct {
		OK      bool  `json:"ok"`
		Checked int64 `json:"checked"`
	}
	if code := admin.json("GET", env.admin.URL+"/api/admin/audit/verify", nil, &verify); code != http.StatusOK || !verify.OK {
		t.Fatalf("audit verify code=%d ok=%v", code, verify.OK)
	}
	if verify.Checked < 4 {
		t.Fatalf("audit verify checked=%d (want >=4)", verify.Checked)
	}

	// Tampering with a stored entry breaks the chain.
	if _, err := env.app.DB().Exec(`UPDATE audit_log SET details = '{"tampered":true}' WHERE id = (SELECT MIN(id) FROM audit_log)`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	var broken struct {
		OK bool `json:"ok"`
	}
	if code := admin.json("GET", env.admin.URL+"/api/admin/audit/verify", nil, &broken); code != http.StatusOK || broken.OK {
		t.Fatalf("tampered chain verify code=%d ok=%v (want ok=false)", code, broken.OK)
	}
}

func TestShareTrashRestore(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)
	pub := newClient(t)
	pub.login(env.public.URL, "public")

	var cfg struct {
		Domains []struct {
			ID int64 `json:"id"`
		} `json:"domains"`
	}
	pub.json("GET", env.public.URL+"/api/public/config", nil, &cfg)

	var created struct {
		Share struct {
			ID string `json:"id"`
		} `json:"share"`
	}
	if code := pub.json("POST", env.public.URL+"/api/public/shares", map[string]any{
		"type": "text", "domainId": cfg.Domains[0].ID, "slug": "trashme",
		"title": "Trash me", "textContent": "x", "expiresAt": 0, "downloadLimit": 0,
	}, &created); code != http.StatusCreated {
		t.Fatalf("create code=%d", code)
	}
	id := created.Share.ID

	// Trash returns 204.
	if resp, _ := pub.do("DELETE", env.public.URL+"/api/public/shares/"+id, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("trash code=%d (want 204)", resp.StatusCode)
	}
	// Trashing again conflicts.
	if resp, _ := pub.do("DELETE", env.public.URL+"/api/public/shares/"+id, nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("double trash code=%d (want 409)", resp.StatusCode)
	}
	// Restore requires an explicit expiry.
	if code := pub.json("POST", env.public.URL+"/api/public/shares/"+id+"/restore", map[string]any{}, nil); code != http.StatusBadRequest {
		t.Fatalf("restore without expiry code=%d (want 400)", code)
	}
	var restored struct {
		Share struct {
			Status string `json:"status"`
		} `json:"share"`
	}
	if code := pub.json("POST", env.public.URL+"/api/public/shares/"+id+"/restore", map[string]any{"expiresAt": 0}, &restored); code != http.StatusOK || restored.Share.Status != "active" {
		t.Fatalf("restore code=%d status=%q", code, restored.Share.Status)
	}
}
