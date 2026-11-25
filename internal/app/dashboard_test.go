package app

import (
	"net/http"
	"testing"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
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
	// The redesign charts a continuous seven-day series and surfaces recent
	// activity plus backup status; all must be present (possibly empty).
	series, ok := out["series"].([]any)
	if !ok || len(series) != 7 {
		t.Fatalf("series must have exactly 7 days, got %T len=%d", out["series"], len(series))
	}
	if _, ok := out["activities"].([]any); !ok {
		t.Fatalf("activities must be a list, got %T", out["activities"])
	}
	backup, ok := out["backup"].(map[string]any)
	if !ok {
		t.Fatalf("backup summary missing, got %T", out["backup"])
	}
	if _, ok := backup["count"]; !ok {
		t.Fatal("backup summary must include count")
	}
	system, ok := out["system"].(map[string]any)
	if !ok {
		t.Fatalf("system missing, got %T", out["system"])
	}
	// Host memory is reported on the supported platforms; assert the contract
	// only where the platform can actually provide it.
	if _, _, supported := memUsage(); supported {
		for _, key := range []string{"memUsedBytes", "memTotalBytes"} {
			if _, ok := system[key]; !ok {
				t.Fatalf("system missing host metric %s", key)
			}
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

// The traffic flush folds counter deltas into traffic_daily and survives a
// process restart, which resets the in-memory counters.
func TestFlushTrafficFoldsDeltas(t *testing.T) {
	env := newTestEnv(t)
	day := util.Day(util.Now())
	read := func() (int64, int64) {
		var tx, rx int64
		if err := env.app.db.QueryRow(`SELECT tx_bytes, rx_bytes FROM traffic_daily WHERE day = ?`, day).Scan(&tx, &rx); err != nil {
			t.Fatalf("traffic_daily row missing: %v", err)
		}
		return tx, rx
	}

	env.app.trafficTx = 1000
	env.app.trafficRx = 200
	env.app.flushTraffic()
	if tx, rx := read(); tx != 1000 || rx != 200 {
		t.Fatalf("first flush tx=%d rx=%d", tx, rx)
	}

	// More traffic in the same window accumulates.
	env.app.trafficTx = 1500
	env.app.trafficRx = 200
	env.app.flushTraffic()
	if tx, rx := read(); tx != 1500 || rx != 200 {
		t.Fatalf("accumulate tx=%d rx=%d", tx, rx)
	}

	// A restart resets the counters; the whole current value is attributed.
	env.app.trafficTx = 50
	env.app.trafficRx = 10
	env.app.flushTraffic()
	if tx, rx := read(); tx != 1550 || rx != 210 {
		t.Fatalf("after reset tx=%d rx=%d", tx, rx)
	}
}
