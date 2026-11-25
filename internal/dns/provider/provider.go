// Package provider defines the DNS management abstraction used by TEX Share.
//
// A provider manages DNS records at a specific DNS host. Implementations must
// never assume Cloudflare: the manual provider is a first-class citizen and
// works for any host that has no API at all. All provider calls take a context
// and must be safe to call concurrently.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Record types understood by the abstraction.
const (
	TypeA     = "A"
	TypeAAAA  = "AAAA"
	TypeCNAME = "CNAME"
	TypeTXT   = "TXT"
)

// ErrUnsupported is returned by providers that cannot manage records through an
// API (for example the manual provider). Callers must fall back to showing
// manual instructions instead of pretending a record was created.
var ErrUnsupported = errors.New("dns provider does not support this operation")

// ErrNotConfigured is returned when a provider is selected but lacks the
// credentials or configuration required to talk to its API.
var ErrNotConfigured = errors.New("dns provider is not configured")

// Record is a single DNS record.
type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied bool   `json:"proxied,omitempty"`
}

// Zone is a DNS zone (a registrable domain) hosted by the provider.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// Provider manages DNS records at one host.
type Provider interface {
	// Name is a stable identifier, e.g. "cloudflare" or "manual".
	Name() string
	// Test verifies credentials/connectivity. It returns ErrUnsupported for
	// providers without an API.
	Test(ctx context.Context) error
	// Zones lists zones visible to the credentials.
	Zones(ctx context.Context) ([]Zone, error)
	// FindRecord returns the record for name/type, if any.
	FindRecord(ctx context.Context, zoneID, name, rtype string) (*Record, bool, error)
	// CreateRecord creates a record.
	CreateRecord(ctx context.Context, zoneID string, rec Record) (Record, error)
	// UpdateRecord updates an existing record by ID.
	UpdateRecord(ctx context.Context, zoneID, recordID string, rec Record) (Record, error)
	// DeleteRecord removes a record by ID.
	DeleteRecord(ctx context.Context, zoneID, recordID string) error
}

// Upsert creates or updates the record for rec.Name/rec.Type. It returns the
// resulting record and whether it was newly created.
func Upsert(ctx context.Context, p Provider, zoneID string, rec Record) (Record, bool, error) {
	existing, found, err := p.FindRecord(ctx, zoneID, rec.Name, rec.Type)
	if err != nil {
		return Record{}, false, err
	}
	if found && existing != nil {
		// Never touch a record that already points somewhere else unless the
		// caller explicitly wants it replaced: that is handled by the caller,
		// which only calls Upsert for records it owns.
		out, err := p.UpdateRecord(ctx, zoneID, existing.ID, rec)
		return out, false, err
	}
	out, err := p.CreateRecord(ctx, zoneID, rec)
	return out, true, err
}

// MatchZone returns the most specific zone that is a suffix of hostname.
func MatchZone(zones []Zone, hostname string) (Zone, bool) {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	best := Zone{}
	found := false
	for _, z := range zones {
		name := strings.ToLower(strings.TrimSuffix(z.Name, "."))
		if name == "" {
			continue
		}
		if host == name || strings.HasSuffix(host, "."+name) {
			if !found || len(name) > len(best.Name) {
				best = z
				found = true
			}
		}
	}
	return best, found
}

// RelativeName returns the record name relative to a zone ("@" for the apex).
func RelativeName(hostname, zone string) string {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	if host == zone {
		return "@"
	}
	if strings.HasSuffix(host, "."+zone) {
		return strings.TrimSuffix(host, "."+zone)
	}
	return host
}

// SortZones orders zones by name for stable UI output.
func SortZones(zones []Zone) {
	sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })
}

// Registry maps provider names to constructor functions.
type Registry struct {
	factories map[string]func() Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]func() Provider{}}
}

// Register adds a provider factory under name.
func (r *Registry) Register(name string, factory func() Provider) {
	r.factories[strings.ToLower(name)] = factory
}

// Names returns the registered provider names, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.factories))
	for name := range r.factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Build constructs a provider by name.
func (r *Registry) Build(name string) (Provider, error) {
	factory, ok := r.factories[strings.ToLower(strings.TrimSpace(name))]
	if !ok || factory == nil {
		return nil, fmt.Errorf("unknown dns provider %q", name)
	}
	return factory(), nil
}
