// Package proxy contains everything that is specific to reverse proxies:
//
//   - safe parsing of forwarded headers, only ever trusted from explicitly
//     configured proxy addresses/CIDRs;
//   - the catalogue of supported external proxies and ready-to-use
//     configuration guides that match TEX Share's port concept (public UI on
//     3535, admin UI on 3636).
//
// It deliberately has no dependency on the rest of the application so it can be
// unit-tested in isolation.
package proxy

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
)

/* -------------------------------------------------------------------------- */
/* Trusted proxies                                                            */
/* -------------------------------------------------------------------------- */

// Trusted is a set of proxy addresses/CIDRs whose forwarded headers may be
// believed. An empty set trusts nobody.
type Trusted struct {
	nets []*net.IPNet
}

// ParseTrusted parses a list of IPs and CIDRs. Invalid entries are ignored.
func ParseTrusted(specs []string) *Trusted {
	t := &Trusted{}
	for _, raw := range specs {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if strings.Contains(s, "/") {
				if _, n, err := net.ParseCIDR(s); err == nil {
					t.nets = append(t.nets, n)
				}
				continue
			}
			if ip := net.ParseIP(s); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				t.nets = append(t.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			}
		}
	}
	return t
}

// Empty reports whether no proxy is trusted.
func (t *Trusted) Empty() bool { return t == nil || len(t.nets) == 0 }

// Contains reports whether ip belongs to a trusted range.
func (t *Trusted) Contains(ip net.IP) bool {
	if t == nil || ip == nil {
		return false
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Strings renders the trusted ranges, sorted, for display.
func (t *Trusted) Strings() []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.nets))
	for _, n := range t.nets {
		out = append(out, n.String())
	}
	sort.Strings(out)
	return out
}

// RemoteIP returns the immediate peer address of a request.
func RemoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host)
}

// ClientIP returns the best-known client address. Forwarded headers are only
// consulted when the immediate peer is a trusted proxy; otherwise the peer
// address is returned unchanged. This prevents clients from spoofing their
// address to bypass rate limiting or auditing.
func ClientIP(r *http.Request, t *Trusted) string {
	peer := RemoteIP(r)
	if t.Empty() || !t.Contains(peer) {
		if peer != nil {
			return peer.String()
		}
		return r.RemoteAddr
	}
	// Cloudflare sets CF-Connecting-IP; it is only meaningful coming from a
	// trusted peer (the tunnel or the proxy).
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
		if ip := net.ParseIP(cf); ip != nil {
			return ip.String()
		}
	}
	chain := forwardedForChain(r)
	// Walk from the closest hop (right) towards the client (left) and return the
	// first address that is not itself a trusted proxy.
	for i := len(chain) - 1; i >= 0; i-- {
		ip := net.ParseIP(chain[i])
		if ip == nil {
			continue
		}
		if !t.Contains(ip) {
			return ip.String()
		}
	}
	if len(chain) > 0 {
		if ip := net.ParseIP(chain[0]); ip != nil {
			return ip.String()
		}
	}
	if peer != nil {
		return peer.String()
	}
	return r.RemoteAddr
}

// forwardedForChain returns the client->proxy address chain from X-Forwarded-For
// (preferred) or the RFC 7239 Forwarded header.
func forwardedForChain(r *http.Request) []string {
	out := []string{}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, strings.Trim(v, "[]"))
			}
		}
		return out
	}
	for _, h := range r.Header.Values("Forwarded") {
		for _, elem := range strings.Split(h, ",") {
			for _, kv := range strings.Split(elem, ";") {
				kv = strings.TrimSpace(kv)
				if !strings.HasPrefix(strings.ToLower(kv), "for=") {
					continue
				}
				v := strings.TrimSpace(kv[4:])
				v = strings.Trim(v, `"`)
				// Strip a bracketed IPv6 or a port.
				if strings.HasPrefix(v, "[") {
					if i := strings.Index(v, "]"); i >= 0 {
						v = v[1:i]
					}
				} else if h, _, err := net.SplitHostPort(v); err == nil {
					v = h
				}
				if v != "" {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// Scheme returns the effective request scheme, honouring X-Forwarded-Proto only
// from a trusted proxy.
func Scheme(r *http.Request, t *Trusted) string {
	if !t.Empty() && t.Contains(RemoteIP(r)) {
		if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p != "" {
			if i := strings.IndexByte(p, ','); i >= 0 {
				p = strings.TrimSpace(p[:i])
			}
			p = strings.ToLower(p)
			if p == "https" || p == "http" {
				return p
			}
		}
		if p := forwardedProto(r); p != "" {
			return p
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// IsHTTPS reports whether the request reached the client over HTTPS.
func IsHTTPS(r *http.Request, t *Trusted) bool { return Scheme(r, t) == "https" }

func forwardedProto(r *http.Request) string {
	for _, h := range r.Header.Values("Forwarded") {
		for _, elem := range strings.Split(h, ",") {
			for _, kv := range strings.Split(elem, ";") {
				kv = strings.TrimSpace(kv)
				if !strings.HasPrefix(strings.ToLower(kv), "proto=") {
					continue
				}
				v := strings.ToLower(strings.Trim(strings.TrimSpace(kv[6:]), `"`))
				if v == "https" || v == "http" {
					return v
				}
			}
		}
	}
	return ""
}

// ForwardedHost returns the original host, honouring X-Forwarded-Host only from
// a trusted proxy. Host validation continues to use the request Host header.
func ForwardedHost(r *http.Request, t *Trusted) string {
	if !t.Empty() && t.Contains(RemoteIP(r)) {
		if h := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); h != "" {
			if i := strings.IndexByte(h, ','); i >= 0 {
				h = strings.TrimSpace(h[:i])
			}
			if h != "" {
				return h
			}
		}
	}
	return r.Host
}

/* -------------------------------------------------------------------------- */
/* Reverse proxy catalogue and guides                                         */
/* -------------------------------------------------------------------------- */

// Kind describes a supported reverse proxy.
type Kind struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	DocsURL string `json:"docsUrl,omitempty"`
}

// Kinds lists the supported reverse proxies, in display order.
var Kinds = []Kind{
	{ID: "none", Label: "Kein externer Reverse Proxy"},
	{ID: "nginx", Label: "Nginx", DocsURL: "https://nginx.org/en/docs/"},
	{ID: "caddy", Label: "Caddy", DocsURL: "https://caddyserver.com/docs/"},
	{ID: "traefik", Label: "Traefik", DocsURL: "https://doc.traefik.io/traefik/"},
	{ID: "npm", Label: "Nginx Proxy Manager", DocsURL: "https://nginxproxymanager.com/"},
	{ID: "haproxy", Label: "HAProxy", DocsURL: "https://docs.haproxy.org/"},
	{ID: "other", Label: "Eigener / anderer Proxy"},
}

// KindIDs returns the valid proxy kind identifiers.
func KindIDs() []string {
	out := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		out = append(out, k.ID)
	}
	return out
}

// ValidKind reports whether id is a known proxy kind.
func ValidKind(id string) bool {
	for _, k := range Kinds {
		if k.ID == id {
			return true
		}
	}
	return false
}

// Params parameterise a guide.
type Params struct {
	Hostname      string // public hostname, e.g. share.example.com
	AdminHostname string // optional admin hostname
	UpstreamHost  string // where TEX Share listens, default 127.0.0.1
	UpstreamPort  string // default 3535
	TrustedCIDR   string // e.g. 127.0.0.1/32,172.16.0.0/12
}

func (p Params) host() string {
	if strings.TrimSpace(p.Hostname) == "" {
		return "share.example.com"
	}
	return strings.TrimSpace(p.Hostname)
}

func (p Params) upHost() string {
	if strings.TrimSpace(p.UpstreamHost) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(p.UpstreamHost)
}

func (p Params) upPort() string {
	if strings.TrimSpace(p.UpstreamPort) == "" {
		return "3535"
	}
	return strings.TrimSpace(p.UpstreamPort)
}

// Guide is a rendered configuration guide.
type Guide struct {
	Kind   string   `json:"kind"`
	Label  string   `json:"label"`
	Config string   `json:"config"`
	Notes  []string `json:"notes"`
}

// BuildGuide renders the configuration snippet for a proxy kind.
func BuildGuide(kind string, p Params) Guide {
	label := kind
	for _, k := range Kinds {
		if k.ID == kind {
			label = k.Label
		}
	}
	switch kind {
	case "nginx":
		return Guide{Kind: kind, Label: label, Config: nginxGuide(p), Notes: nginxNotes()}
	case "caddy":
		return Guide{Kind: kind, Label: label, Config: caddyGuide(p), Notes: caddyNotes()}
	case "traefik":
		return Guide{Kind: kind, Label: label, Config: traefikGuide(p), Notes: traefikNotes()}
	case "npm":
		return Guide{Kind: kind, Label: label, Config: npmGuide(p), Notes: npmNotes()}
	case "haproxy":
		return Guide{Kind: kind, Label: label, Config: haproxyGuide(p), Notes: haproxyNotes()}
	default:
		return Guide{Kind: kind, Label: label, Config: genericGuide(p), Notes: genericNotes()}
	}
}

func nginxGuide(p Params) string {
	return fmt.Sprintf(`# /etc/nginx/conf.d/texshare.conf
server {
    listen 443 ssl http2;
    server_name %s;

    ssl_certificate     /etc/letsencrypt/live/%s/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/%s/privkey.pem;

    # Große und resumable Uploads zulassen.
    client_max_body_size 0;
    proxy_request_buffering off;   # Request-Body direkt streamen
    proxy_buffering off;           # Downloads ohne Puffer streamen

    location / {
        proxy_pass http://%s:%s;
        proxy_http_version 1.1;

        # Host und Forwarded-Header weitergeben.
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host  $host;

        # Range Requests (Resume, Video-Seeking) durchreichen.
        proxy_set_header Range    $http_range;
        proxy_set_header If-Range $http_if_range;

        # Langlaufende Transfers nicht abschneiden.
        proxy_connect_timeout 60s;
        proxy_send_timeout    3600s;
        proxy_read_timeout    3600s;
    }
}`, p.host(), p.host(), p.host(), p.upHost(), p.upPort())
}

func nginxNotes() []string {
	return []string{
		"Trage die Proxy-Adresse (z. B. 127.0.0.1/32 oder das Docker-Netz) unter „Vertrauenswürdige Proxies“ ein.",
		"client_max_body_size 0 hebt das Request-Limit auf – nötig für große Uploads.",
		"proxy_request_buffering off und proxy_buffering off verhindern Pufferung bei Streams.",
		"Die Admin-UI (Port 3636) wird hier absichtlich nicht veröffentlicht.",
	}
}

func caddyGuide(p Params) string {
	return fmt.Sprintf(`# Caddyfile
%s {
    encode zstd gzip

    reverse_proxy %s:%s {
        header_up Host              {host}
        header_up X-Forwarded-For   {remote_host}
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-Host  {host}

        # Nicht puffern -> Streaming und große Dateien.
        flush_interval -1

        transport http {
            dial_timeout  10s
            read_timeout  1h
            write_timeout 1h
        }
    }
}`, p.host(), p.upHost(), p.upPort())
}

func caddyNotes() []string {
	return []string{
		"Caddy besorgt TLS-Zertifikate automatisch (Let's Encrypt / ZeroSSL).",
		"flush_interval -1 deaktiviert das Antwort-Buffering für Streaming.",
		"Caddy überträgt Range-Requests und Host-Header standardkonform.",
		"Proxy-IP (Docker-Bridge oder 127.0.0.1) unter „Vertrauenswürdige Proxies“ eintragen.",
	}
}

func traefikGuide(p Params) string {
	return fmt.Sprintf(`# Dynamische Konfiguration (file provider)
http:
  routers:
    texshare:
      rule: "Host(`+"`"+`%s`+"`"+`)"
      entryPoints: [websecure]
      service: texshare
      tls:
        certResolver: letsencrypt

  services:
    texshare:
      loadBalancer:
        passHostHeader: true
        servers:
          - url: "http://%s:%s"
        responseForwarding:
          flushInterval: 100ms   # Streaming statt Pufferung`, p.host(), p.upHost(), p.upPort())
}

func traefikNotes() []string {
	return []string{
		"passHostHeader: true erhält den Host-Header; Traefik setzt X-Forwarded-* automatisch.",
		"flushInterval sorgt für sofortiges Streaming großer Dateien.",
		"Traefik puffert Request-Bodies standardmäßig nicht – resumable Uploads funktionieren.",
		"Für Docker-Nutzung die Proxy-Netz-CIDR unter „Vertrauenswürdige Proxies“ eintragen.",
	}
}

func npmGuide(p Params) string {
	return fmt.Sprintf(`# Nginx Proxy Manager – „Add Proxy Host“
Domain Names:            %s
Scheme:                  http
Forward Hostname / IP:   %s
Forward Port:            %s
Block Common Exploits:   an
Websockets Support:      an
SSL:                     Let's Encrypt anfordern, Force SSL + HTTP/2 an

# Tab „Advanced“ – Custom Nginx Configuration:
client_max_body_size 0;
proxy_request_buffering off;
proxy_buffering off;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-Host  $host;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;`, p.host(), p.upHost(), p.upPort())
}

func npmNotes() []string {
	return []string{
		"Im „Advanced“-Tab die Custom-Konfiguration eintragen, damit große Uploads und Streaming funktionieren.",
		"NPM läuft selbst als Container: die NPM-Container-IP bzw. das Docker-Netz unter „Vertrauenswürdige Proxies“ eintragen.",
		"Admin-UI (3636) nicht als Proxy Host anlegen.",
	}
}

func haproxyGuide(p Params) string {
	return fmt.Sprintf(`# /etc/haproxy/haproxy.cfg (Auszug)
frontend https_in
    bind *:443 ssl crt /etc/haproxy/certs/%s.pem
    http-request set-header X-Forwarded-Proto https
    http-request set-header X-Forwarded-Host  %%[req.hdr(host)]
    default_backend texshare

backend texshare
    balance roundrobin
    option forwardfor                 # X-Forwarded-For setzen
    option http-server-close
    http-reuse safe
    timeout connect 10s
    timeout server  1h                # große Uploads/Downloads
    timeout client  1h
    server texshare1 %s:%s check`, p.host(), p.upHost(), p.upPort())
}

func haproxyNotes() []string{
	return []string{
		"option forwardfor setzt X-Forwarded-For; der Host-Header bleibt erhalten.",
		"timeout server/client 1h verhindert Abbrüche bei großen Transfers.",
		"HAProxy puffert keine Request-Bodies – resumable Uploads funktionieren.",
		"Proxy-IP unter „Vertrauenswürdige Proxies“ eintragen.",
	}
}

func genericGuide(p Params) string {
	return fmt.Sprintf(`# Generischer Reverse Proxy
Upstream:        http://%s:%s
Öffentlicher Name: %s
TLS:             am Proxy terminieren (Let's Encrypt o. ä.)

Pflicht-Header an TEX Share weitergeben:
  Host:              %s
  X-Forwarded-For:   <Client-IP>
  X-Forwarded-Proto: https
  X-Forwarded-Host:  %s

Wichtig:
  - Request-Body nicht puffern (Streaming)
  - Kein Request-Size-Limit (große/resumable Uploads)
  - Range-/If-Range-Header durchreichen
  - Read-/Send-Timeout >= 1h`, p.upHost(), p.upPort(), p.host(), p.host(), p.host())
}

func genericNotes() []string {
	return []string{
		"Jeder standardkonforme HTTP(S)-Proxy funktioniert, solange Host- und Forwarded-Header gesetzt werden.",
		"Die Proxy-Adresse unter „Vertrauenswürdige Proxies“ eintragen, sonst werden Forwarded-Header ignoriert.",
		"Die Admin-UI läuft weiterhin separat auf Port 3636.",
	}
}
