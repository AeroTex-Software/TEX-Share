// Package manual is the DNS provider used when records are managed by hand at
// an arbitrary DNS host that offers no API. It never pretends to create
// records: every API operation returns provider.ErrUnsupported, and the admin
// UI shows a concrete instruction table instead.
package manual

import (
	"context"
	"fmt"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/dns/provider"
)

// Provider is the manual DNS provider.
type Provider struct{}

// New returns the manual provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (p *Provider) Name() string { return "manual" }

// Test always succeeds: there is nothing to connect to.
func (p *Provider) Test(ctx context.Context) error { return nil }

// Zones returns ErrUnsupported.
func (p *Provider) Zones(ctx context.Context) ([]provider.Zone, error) { return nil, provider.ErrUnsupported }

// FindRecord returns ErrUnsupported.
func (p *Provider) FindRecord(ctx context.Context, zoneID, name, rtype string) (*provider.Record, bool, error) {
	return nil, false, provider.ErrUnsupported
}

// CreateRecord returns ErrUnsupported.
func (p *Provider) CreateRecord(ctx context.Context, zoneID string, rec provider.Record) (provider.Record, error) {
	return provider.Record{}, provider.ErrUnsupported
}

// UpdateRecord returns ErrUnsupported.
func (p *Provider) UpdateRecord(ctx context.Context, zoneID, recordID string, rec provider.Record) (provider.Record, error) {
	return provider.Record{}, provider.ErrUnsupported
}

// DeleteRecord returns ErrUnsupported.
func (p *Provider) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return provider.ErrUnsupported
}

// Instruction is one required DNS record the administrator must create.
type Instruction struct {
	Type    string `json:"type"`
	Name    string `json:"name"`    // relative name (e.g. "share" or "@")
	Content string `json:"content"` // target value
	Hint    string `json:"hint,omitempty"`
}

// Instructions builds the record table for a hostname given the chosen network
// path. It is pure: no network access, no guessing about the DNS host.
//
//   - direct / static / ddns: an A (and optional AAAA) record to the public IP.
//   - proxy: the proxy terminates TLS and forwards to TEX Share, so the record
//     points at the proxy (its public IP) — or a CNAME to it.
//   - tunnel: a CNAME to the tunnel hostname is created by Cloudflare; the
//     caller passes tunnelTarget.
func Instructions(hostname, networkMode, recordType, ipv4, ipv6, tunnelTarget string) []Instruction {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	zone := guessZone(hostname)
	name := provider.RelativeName(hostname, zone)

	out := []Instruction{}
	switch strings.ToLower(networkMode) {
	case "tunnel":
		target := tunnelTarget
		if target == "" {
			target = "<tunnel-id>.cfargotunnel.com"
		}
		out = append(out, Instruction{Type: provider.TypeCNAME, Name: name, Content: target,
			Hint: "Wird beim Cloudflare-Tunnel automatisch angelegt (proxied)."})
	case "proxy":
		// The proxy owns the public name; point it at the proxy.
		if recordType == provider.TypeCNAME {
			out = append(out, Instruction{Type: provider.TypeCNAME, Name: name, Content: "<proxy-hostname>",
				Hint: "CNAME auf den Hostnamen des Reverse Proxys."})
		} else {
			if ipv4 != "" {
				out = append(out, Instruction{Type: provider.TypeA, Name: name, Content: ipv4,
					Hint: "Öffentliche IPv4 des Reverse Proxys."})
			}
			if ipv6 != "" {
				out = append(out, Instruction{Type: provider.TypeAAAA, Name: name, Content: ipv6})
			}
		}
	default: // direct, static, ddns
		if recordType == provider.TypeCNAME {
			out = append(out, Instruction{Type: provider.TypeCNAME, Name: name, Content: "<ziel-hostname>"})
		} else {
			if ipv4 != "" {
				out = append(out, Instruction{Type: provider.TypeA, Name: name, Content: ipv4})
			} else {
				out = append(out, Instruction{Type: provider.TypeA, Name: name, Content: "<öffentliche IPv4>"})
			}
			if ipv6 != "" {
				out = append(out, Instruction{Type: provider.TypeAAAA, Name: name, Content: ipv6})
			}
		}
	}
	return out
}

// guessZone derives the registrable-ish zone from a hostname. It strips the
// left-most label only (example.com for share.example.com); for deeper names it
// keeps the last two labels. This is a best-effort display helper, never used
// for API calls.
func guessZone(hostname string) string {
	labels := strings.Split(strings.TrimSuffix(hostname, "."), ".")
	if len(labels) <= 2 {
		return hostname
	}
	// Common multi-part public suffixes are handled by keeping two labels, which
	// matches the display expectation for the vast majority of domains.
	return strings.Join(labels[len(labels)-2:], ".")
}

// Summary renders a human-readable one-liner for logs and audit entries.
func Summary(hostname, mode string) string {
	return fmt.Sprintf("%s (%s)", hostname, mode)
}
