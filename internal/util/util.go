// Package util provides small, dependency-free helpers shared by the app.
package util

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RandomToken returns n random bytes encoded with URL-safe base64 (no padding).
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomHex returns n random bytes as lowercase hex.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// HashToken hashes a bearer token with SHA-256 for database storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ConstantEqual compares two strings in constant time.
func ConstantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Now returns the current Unix timestamp.
func Now() int64 { return time.Now().Unix() }

// FormatTime renders a Unix timestamp as RFC3339 or empty for zero.
func FormatTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// JSON encodes a value to a string, falling back to "{}".
func JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// HumanBytes formats a byte count.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Slugify creates a conservative URL slug. Empty input yields "".
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		case r >= 0x00C0 && r <= 0x024F:
			// Strip common accents by keeping ASCII base letters.
			base := strings.Map(func(x rune) rune {
				switch x {
				case 'ä', 'à', 'á', 'â', 'ã', 'å':
					return 'a'
				case 'ö', 'ò', 'ó', 'ô', 'õ', 'ø':
					return 'o'
				case 'ü', 'ù', 'ú', 'û':
					return 'u'
				case 'é', 'è', 'ê', 'ë':
					return 'e'
				case 'í', 'ì', 'î', 'ï':
					return 'i'
				case 'ç':
					return 'c'
				case 'ñ':
					return 'n'
				case 'ß':
					return 's'
				}
				return x
			}, string(r))
			b.WriteString(base)
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
		out = strings.TrimRight(out, "-")
	}
	return out
}

var slugRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,62}[a-z0-9])?$`)
var namespaceRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,30}[a-z0-9])?$`)

// ValidSlug reports whether s is an acceptable share slug.
func ValidSlug(s string) bool { return slugRE.MatchString(s) }

// ValidNamespace reports whether s is an acceptable personal namespace.
func ValidNamespace(s string) bool { return namespaceRE.MatchString(s) }

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)

// ValidUsername validates administrator-assigned account names.
func ValidUsername(s string) bool { return usernameRE.MatchString(s) }

var hostnameRE = regexp.MustCompile(`^(?i)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidHostname validates a DNS hostname (no scheme, no port, no path).
func ValidHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	if strings.Contains(s, ":") || strings.Contains(s, "/") {
		return false
	}
	return hostnameRE.MatchString(s)
}

// ClientIP extracts the client address. Forwarded headers are only trusted
// when explicitly enabled by configuration.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
			return strings.TrimSpace(cf)
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// SanitizeFilename removes control characters and path separators while
// preserving a human readable name for Content-Disposition headers.
func SanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "download"
	}
	if len(name) > 180 {
		ext := ""
		if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 12 {
			ext = name[i:]
		}
		name = name[:180-len(ext)] + ext
	}
	return name
}

// ContentDisposition builds an RFC 6266 header value.
func ContentDisposition(kind, name string) string {
	name = SanitizeFilename(name)
	ascii := strings.Map(func(r rune) rune {
		if r >= 32 && r < 127 && r != '"' && r != '\\' {
			return r
		}
		return '_'
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, escapeRFC5987(name))
}

func escapeRFC5987(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '!' || c == '#' || c == '$' || c == '&' || c == '+' || c == '-' || c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%")
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

// ParseInt parses an integer with a fallback.
func ParseInt(s string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return v
}

// ParseBool parses a boolean with a fallback.
func ParseBool(s string, def bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

// Truncate shortens a string for safe logging.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// EscapeHTML is a helper for the rare places where HTML is assembled manually.
func EscapeHTML(s string) string { return html.EscapeString(s) }

// Day returns the UTC day string for a timestamp.
func Day(ts int64) string { return time.Unix(ts, 0).UTC().Format("2006-01-02") }
