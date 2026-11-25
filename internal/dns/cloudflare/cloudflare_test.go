package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AeroTex-Software/TEX-Share/internal/dns/provider"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("test-token").WithBaseURL(srv.URL)
}

func envelope(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}

func TestTestVerifiesToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/tokens/verify" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		envelope(t, w, map[string]any{"status": "active"})
	})
	if err := c.Test(context.Background()); err != nil {
		t.Fatalf("Test: %v", err)
	}
}

func TestNotConfiguredWithoutToken(t *testing.T) {
	c := New("")
	if err := c.Test(context.Background()); err != provider.ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestZonesAndRecords(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zones":
			envelope(t, w, []map[string]any{{"id": "z1", "name": "example.com", "status": "active"}})
		case "/zones/z1/dns_records":
			if r.Method == http.MethodPost {
				envelope(t, w, map[string]any{"id": "r1", "type": "A", "name": "share.example.com", "content": "203.0.113.10"})
				return
			}
			envelope(t, w, []map[string]any{})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	zones, err := c.Zones(context.Background())
	if err != nil || len(zones) != 1 || zones[0].ID != "z1" {
		t.Fatalf("Zones = %+v err=%v", zones, err)
	}
	rec, err := c.CreateRecord(context.Background(), "z1", provider.Record{Type: "A", Name: "share.example.com", Content: "203.0.113.10"})
	if err != nil || rec.ID != "r1" {
		t.Fatalf("CreateRecord = %+v err=%v", rec, err)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []map[string]any{{"code": 9109, "message": "Invalid access token"}},
		})
	})
	err := c.Test(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if apiErr, ok := err.(*APIError); !ok || apiErr.Message != "Invalid access token" {
		t.Fatalf("unexpected error: %v", err)
	}
}
