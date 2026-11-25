package app

import (
	"net/http"
	"testing"
)

func TestNetworkStatusAndSettingsHidden(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	var status struct {
		Config     map[string]string `json:"config"`
		Providers  []string          `json:"providers"`
		ProxyKinds []struct {
			ID string `json:"id"`
		} `json:"proxyKinds"`
		PublicPort string `json:"publicPort"`
	}
	if code := admin.json("GET", env.admin.URL+"/api/admin/network/status", nil, &status); code != http.StatusOK {
		t.Fatalf("network status code=%d", code)
	}
	if len(status.Providers) == 0 || len(status.ProxyKinds) == 0 {
		t.Fatalf("expected providers and proxy kinds, got %+v", status)
	}
	if status.PublicPort == "" {
		t.Fatalf("publicPort must be reported")
	}

	// The encrypted Cloudflare token must never leave the server.
	var settingsResp struct {
		Settings map[string]string `json:"settings"`
	}
	if code := admin.json("GET", env.admin.URL+"/api/admin/settings", nil, &settingsResp); code != http.StatusOK {
		t.Fatalf("settings code=%d", code)
	}
	if _, leaked := settingsResp.Settings["cf_api_token_enc"]; leaked {
		t.Fatal("cf_api_token_enc must not be exposed via the settings API")
	}
}

func TestNetworkPatchValidatesTrustedProxies(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	// Invalid CIDR is rejected.
	var out map[string]any
	code := admin.json("PATCH", env.admin.URL+"/api/admin/network", map[string]any{
		"settings": map[string]string{"trusted_proxies": "127.0.0.1/33"},
	}, &out)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid CIDR code=%d, want 400", code)
	}

	// Valid values are stored and echoed back.
	code = admin.json("PATCH", env.admin.URL+"/api/admin/network", map[string]any{
		"settings": map[string]string{
			"network_connection": "proxy",
			"network_proxy_kind": "caddy",
			"trusted_proxies":    "127.0.0.1/32, 10.0.0.0/8",
		},
	}, &out)
	if code != http.StatusOK {
		t.Fatalf("valid patch code=%d", code)
	}
	if got := env.app.settings.Get("network_connection"); got != "proxy" {
		t.Fatalf("network_connection=%q want proxy", got)
	}
	if got := env.app.settings.Get("network_proxy_kind"); got != "caddy" {
		t.Fatalf("network_proxy_kind=%q want caddy", got)
	}
}

func TestManualDNSApplyIsRefused(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	var created struct {
		Domain struct {
			ID int64 `json:"id"`
		} `json:"domain"`
	}
	if code := admin.json("POST", env.admin.URL+"/api/admin/domains", map[string]any{
		"hostname": "share.example.com",
	}, &created); code != http.StatusCreated {
		t.Fatalf("create domain code=%d", code)
	}
	if created.Domain.ID == 0 {
		t.Fatal("domain id missing")
	}

	// Default provider is manual -> applying a record must not pretend success.
	var applyOut map[string]any
	code := admin.json("POST", env.admin.URL+"/api/admin/network/dns/apply", map[string]any{
		"domainId": created.Domain.ID,
		"type":     "A",
	}, &applyOut)
	if code != http.StatusBadRequest {
		t.Fatalf("manual dns apply code=%d, want 400", code)
	}
	if applyOut["error"] != "dns_manual" {
		t.Fatalf("unexpected error %v", applyOut["error"])
	}
}

func TestDNSInstructionsEndpoint(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	var out struct {
		Instructions []struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"instructions"`
	}
	code := admin.json("GET", env.admin.URL+"/api/admin/network/dns/instructions?hostname=share.example.com&mode=direct&type=A", nil, &out)
	if code != http.StatusOK {
		t.Fatalf("instructions code=%d", code)
	}
	if len(out.Instructions) != 1 || out.Instructions[0].Name != "share" || out.Instructions[0].Type != "A" {
		t.Fatalf("unexpected instructions: %+v", out.Instructions)
	}
}
