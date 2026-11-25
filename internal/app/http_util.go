package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/skip2/go-qrcode"
)

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func hashDummy() (string, error) {
	return auth.HashPassword("texshare-dummy-" + strings.Repeat("x", 24))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, APIError{Error: code, Message: message})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func qrPNG(content string, size int) (string, error) {
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	code.DisableBorder = false
	png, err := code.PNG(size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

func parsePage(r *http.Request) (page, limit int) {
	page = int(utilParseInt(r.URL.Query().Get("page"), 1))
	if page < 1 {
		page = 1
	}
	limit = int(utilParseInt(r.URL.Query().Get("limit"), 20))
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return
}

func utilParseInt(s string, def int64) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}

func boolFromQuery(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	return v == "1" || strings.EqualFold(v, "true")
}

var errNotFound = errors.New("not found")

func (a *App) domainByID(id int64) (*Domain, error) {
	var d Domain
	var lastOK interface{}
	var dnsOK interface{}
	err := a.db.QueryRow(`SELECT id, hostname, enabled, verified, verification_token, is_default, tls_status,
		COALESCE(last_checked_at,0), last_check_ok, COALESCE(last_check_error,''), created_at,
		dns_provider, dns_record_type, dns_target, dns_zone_id, dns_record_id, dns_status,
		COALESCE(dns_checked_at,0), dns_ok, COALESCE(dns_error,''), network_mode, proxy_kind
		FROM domains WHERE id = ?`, id).
		Scan(&d.ID, &d.Hostname, &d.Enabled, &d.Verified, &d.VerificationToken, &d.IsDefault, &d.TLSStatus, &d.LastCheckedAt, &lastOK, &d.LastCheckError, &d.CreatedAt,
			&d.DNSProvider, &d.DNSRecordType, &d.DNSTarget, &d.DNSZoneID, &d.DNSRecordID, &d.DNSStatus,
			&d.DNSCheckedAt, &dnsOK, &d.DNSError, &d.NetworkMode, &d.ProxyKind)
	if err != nil {
		return nil, err
	}
	if b, ok := lastOK.(int64); ok {
		v := b == 1
		d.LastCheckOK = &v
	}
	if b, ok := dnsOK.(int64); ok {
		v := b == 1
		d.DNSOK = &v
	}
	return &d, nil
}

func (a *App) shareURLPath(s *Share) string {
	parts := []string{}
	if s.Namespace != "" {
		parts = append(parts, s.Namespace)
	}
	parts = append(parts, s.Slug)
	return "/" + strings.Join(parts, "/")
}

func (a *App) shareURL(s *Share) string {
	d, err := a.domainByID(s.DomainID)
	if err != nil {
		return ""
	}
	scheme := "https"
	if a.cfg.Dev {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s%s", scheme, d.Hostname, a.shareURLPath(s))
}
