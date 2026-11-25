package app

import (
	"net/http"
	"testing"
)

// The dashboard must expose storage/usage data but never the audit log.
func TestDashboardOmitsAuditLog(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	var out map[string]any
	if code := admin.json("GET", env.admin.URL+"/api/admin/dashboard", nil, &out); code != http.StatusOK {
		t.Fatalf("dashboard code=%d", code)
	}
	if _, ok := out["recentAudit"]; ok {
		t.Fatal("dashboard must not include the audit log")
	}
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("dashboard must include usage, got %T", out["usage"])
	}
	for _, key := range []string{"storageUsedBytes", "storageReservedBytes"} {
		if _, ok := usage[key]; !ok {
			t.Fatalf("usage missing %s", key)
		}
	}
}

// The live-traffic endpoint reports monotonic byte counters; serving a public
// response must increase the transmitted counter. Admin traffic is excluded.
func TestTrafficCounterIncreases(t *testing.T) {
	env := newTestEnv(t)
	admin := newClient(t)
	admin.setupAdmin(env.admin.URL)

	var before struct {
		Tx int64 `json:"tx"`
		Rx int64 `json:"rx"`
		Ts int64 `json:"ts"`
	}
	if code := admin.json("GET", env.admin.URL+"/api/admin/traffic", nil, &before); code != http.StatusOK {
		t.Fatalf("traffic code=%d", code)
	}
	if before.Ts == 0 {
		t.Fatal("traffic response must carry a timestamp")
	}

	// Fetch a public response so bytes flow through the public middleware.
	var meta map[string]any
	admin.json("GET", env.public.URL+"/api/public/meta", nil, &meta)

	var after struct {
		Tx int64 `json:"tx"`
		Rx int64 `json:"rx"`
		Ts int64 `json:"ts"`
	}
	admin.json("GET", env.admin.URL+"/api/admin/traffic", nil, &after)
	if after.Tx <= before.Tx {
		t.Fatalf("tx did not increase: before=%d after=%d", before.Tx, after.Tx)
	}
}
