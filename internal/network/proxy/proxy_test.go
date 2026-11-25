package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func req(remote string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestParseTrusted(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32", "10.0.0.0/8", "::1", "not-an-ip", " 192.168.1.5 "})
	if tr.Empty() {
		t.Fatal("expected non-empty trusted set")
	}
	cases := map[string]bool{
		"127.0.0.1":   true,
		"10.1.2.3":    true,
		"192.168.1.5": true,
		"192.168.1.6": false,
		"8.8.8.8":     false,
	}
	for ip, want := range cases {
		if got := tr.Contains(net.ParseIP(ip)); got != want {
			t.Errorf("Contains(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestClientIPIgnoresForwardedFromUntrustedPeer(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32"})
	// Peer is public -> headers must be ignored.
	r := req("203.0.113.7:44321", map[string]string{
		"X-Forwarded-For":  "1.2.3.4",
		"CF-Connecting-IP": "5.6.7.8",
	})
	if got := ClientIP(r, tr); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want peer address", got)
	}
}

func TestClientIPUsesForwardedFromTrustedPeer(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32", "10.0.0.0/8"})
	r := req("127.0.0.1:5555", map[string]string{
		"X-Forwarded-For": "198.51.100.9, 10.0.0.2",
	})
	// Walk right-to-left: 10.0.0.2 is trusted, 198.51.100.9 is the client.
	if got := ClientIP(r, tr); got != "198.51.100.9" {
		t.Fatalf("ClientIP = %q, want 198.51.100.9", got)
	}
}

func TestClientIPPrefersCFConnectingIP(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32"})
	r := req("127.0.0.1:5555", map[string]string{
		"CF-Connecting-IP": "198.51.100.42",
		"X-Forwarded-For":  "1.2.3.4",
	})
	if got := ClientIP(r, tr); got != "198.51.100.42" {
		t.Fatalf("ClientIP = %q, want 198.51.100.42", got)
	}
}

func TestSchemeOnlyTrustsForwardedProtoFromProxy(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32"})
	trusted := req("127.0.0.1:5555", map[string]string{"X-Forwarded-Proto": "https"})
	if !IsHTTPS(trusted, tr) {
		t.Fatal("expected https from trusted proxy")
	}
	untrusted := req("203.0.113.7:5555", map[string]string{"X-Forwarded-Proto": "https"})
	if IsHTTPS(untrusted, tr) {
		t.Fatal("forwarded proto must be ignored from an untrusted peer")
	}
}

func TestForwardedHostIgnoredFromUntrustedPeer(t *testing.T) {
	tr := ParseTrusted([]string{"127.0.0.1/32"})
	r := req("203.0.113.7:5555", map[string]string{"X-Forwarded-Host": "evil.example"})
	if got := ForwardedHost(r, tr); got == "evil.example" {
		t.Fatal("forwarded host must be ignored from an untrusted peer")
	}
}

func TestForwardedHeaderFallback(t *testing.T) {
	tr := ParseTrusted([]string{"10.0.0.0/8"})
	r := req("10.0.0.1:5555", map[string]string{
		"Forwarded": `for="198.51.100.5:1234";proto=https`,
	})
	if got := ClientIP(r, tr); got != "198.51.100.5" {
		t.Fatalf("ClientIP = %q, want 198.51.100.5", got)
	}
	if Scheme(r, tr) != "https" {
		t.Fatalf("Scheme = %q, want https", Scheme(r, tr))
	}
}

func TestBuildGuideMentionsPortAndHost(t *testing.T) {
	g := BuildGuide("nginx", Params{Hostname: "share.example.com", UpstreamPort: "3535"})
	if g.Config == "" || g.Label != "Nginx" {
		t.Fatalf("unexpected guide: %+v", g)
	}
	if !contains(g.Config, "share.example.com") || !contains(g.Config, "3535") {
		t.Fatalf("guide missing host/port: %s", g.Config)
	}
}

func TestTraefikGuideBackticks(t *testing.T) {
	g := BuildGuide("traefik", Params{Hostname: "share.example.com"})
	if !contains(g.Config, "Host(`share.example.com`)") {
		t.Fatalf("traefik guide rule malformed:\n%s", g.Config)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
