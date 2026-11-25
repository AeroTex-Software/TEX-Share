package provider

import "testing"

func TestMatchZone(t *testing.T) {
	zones := []Zone{{ID: "1", Name: "example.com"}, {ID: "2", Name: "sub.example.com"}}
	z, ok := MatchZone(zones, "share.sub.example.com")
	if !ok || z.ID != "2" {
		t.Fatalf("expected most specific zone, got %+v ok=%v", z, ok)
	}
	z, ok = MatchZone(zones, "example.com")
	if !ok || z.ID != "1" {
		t.Fatalf("expected apex zone, got %+v ok=%v", z, ok)
	}
	if _, ok := MatchZone(zones, "unrelated.org"); ok {
		t.Fatal("expected no zone match")
	}
}

func TestRelativeName(t *testing.T) {
	cases := []struct{ host, zone, want string }{
		{"example.com", "example.com", "@"},
		{"share.example.com", "example.com", "share"},
		{"a.b.example.com", "example.com", "a.b"},
		{"example.com.", "example.com", "@"},
	}
	for _, c := range cases {
		if got := RelativeName(c.host, c.zone); got != c.want {
			t.Errorf("RelativeName(%q,%q) = %q, want %q", c.host, c.zone, got, c.want)
		}
	}
}
