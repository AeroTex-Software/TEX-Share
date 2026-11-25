// Package cloudflare implements the DNS provider interface against the
// Cloudflare API v4 using an API token. It is used only for DNS record
// management (zones, A/AAAA/CNAME/TXT) and DDNS; the tunnel is managed
// separately through the cloudflared CLI.
//
// The token must be created with the minimal scopes:
//
//	Zone -> DNS    -> Edit
//	Zone -> Zone   -> Read
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/dns/provider"
)

// DefaultBaseURL is the Cloudflare API endpoint.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// Client is a Cloudflare DNS provider bound to one API token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// New returns a client for the given API token. A short timeout is applied so
// a slow API never blocks a request handler indefinitely.
func New(token string) *Client {
	return &Client{
		token:   strings.TrimSpace(token),
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// WithBaseURL overrides the API endpoint (used by tests).
func (c *Client) WithBaseURL(base string) *Client {
	c.baseURL = strings.TrimRight(base, "/")
	return c
}

// Name implements provider.Provider.
func (c *Client) Name() string { return "cloudflare" }

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfEnvelope struct {
	Success  bool            `json:"success"`
	Errors   []cfError       `json:"errors"`
	Messages []any           `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

// APIError is a structured Cloudflare API error.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("cloudflare api error (status %d)", e.Status)
	}
	return e.Message
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if c.token == "" {
		return provider.ErrNotConfigured
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var env cfEnvelope
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &env)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (!env.Success && len(env.Errors) > 0) {
		apiErr := &APIError{Status: resp.StatusCode}
		if len(env.Errors) > 0 {
			apiErr.Code = env.Errors[0].Code
			apiErr.Message = env.Errors[0].Message
		}
		return apiErr
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return err
		}
	}
	return nil
}

// Test verifies the token with the /user/tokens/verify endpoint.
func (c *Client) Test(ctx context.Context) error {
	var out struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, &out); err != nil {
		return err
	}
	if out.Status != "" && out.Status != "active" {
		return fmt.Errorf("api token status is %q", out.Status)
	}
	return nil
}

// Zones lists the zones visible to the token.
func (c *Client) Zones(ctx context.Context) ([]provider.Zone, error) {
	var out []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/zones?per_page=50", nil, &out); err != nil {
		return nil, err
	}
	zones := make([]provider.Zone, 0, len(out))
	for _, z := range out {
		zones = append(zones, provider.Zone{ID: z.ID, Name: z.Name, Status: z.Status})
	}
	provider.SortZones(zones)
	return zones, nil
}

type cfRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func (r cfRecord) toProvider() provider.Record {
	return provider.Record{ID: r.ID, Type: r.Type, Name: r.Name, Content: r.Content, TTL: r.TTL, Proxied: r.Proxied}
}

// FindRecord looks up a single record by name and type.
func (c *Client) FindRecord(ctx context.Context, zoneID, name, rtype string) (*provider.Record, bool, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s&per_page=1", zoneID, rtype, name)
	var out []cfRecord
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, false, err
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	rec := out[0].toProvider()
	return &rec, true, nil
}

// CreateRecord creates a DNS record.
func (c *Client) CreateRecord(ctx context.Context, zoneID string, rec provider.Record) (provider.Record, error) {
	body := cfRecord{Type: rec.Type, Name: rec.Name, Content: rec.Content, TTL: ttlOr(rec.TTL), Proxied: rec.Proxied}
	var out cfRecord
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", body, &out); err != nil {
		return provider.Record{}, err
	}
	return out.toProvider(), nil
}

// UpdateRecord replaces an existing record.
func (c *Client) UpdateRecord(ctx context.Context, zoneID, recordID string, rec provider.Record) (provider.Record, error) {
	body := cfRecord{Type: rec.Type, Name: rec.Name, Content: rec.Content, TTL: ttlOr(rec.TTL), Proxied: rec.Proxied}
	var out cfRecord
	if err := c.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+recordID, body, &out); err != nil {
		return provider.Record{}, err
	}
	return out.toProvider(), nil
}

// DeleteRecord removes a record.
func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
}

func ttlOr(ttl int) int {
	if ttl <= 0 {
		return 1 // "automatic"
	}
	return ttl
}
