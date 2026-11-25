// Package settings stores runtime configuration in SQLite with an in-memory
// cache. Secrets are stored encrypted by the caller.
package settings

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Defaults are safe values used immediately after installation.
var Defaults = map[string]string{
	"setup_completed":              "0",
	"app_name":                     "TEX Share",
	"logo_path":                    "",
	"favicon_path":                 "",
	"default_domain_id":            "0",
	"default_storage_limit_bytes":  "5368709120",
	"allow_unlimited_storage":      "1",
	"global_storage_limit_bytes":   "0",
	"max_file_size_bytes":          "2147483648",
	"max_share_size_bytes":         "5368709120",
	"resumable_threshold_bytes":    "104857600",
	"trash_retention_days":         "30",
	"stats_retention_days":         "180",
	"max_session_hours":            "72",
	"remember_me_days":             "30",
	"share_access_hours":           "12",
	"password_min_length":          "10",
	"login_rate_per_minute":        "10",
	"login_failures_before_lock":   "5",
	"lockout_minutes":              "15",
	"api_rate_per_minute":          "300",
	"upload_concurrency_per_user":  "3",
	"download_concurrency_per_user": "4",
	"global_upload_limit":          "20",
	"global_download_limit":        "60",
	"zip_concurrency":              "2",
	"twofa_attempts":               "5",
	"upload_queue_limit":           "10",
	"antivirus_enabled":            "0",
	"freshclam_interval_hours":     "6",
	"cloudflare_enabled":           "0",
	"cf_tunnel_id":                 "",
	"cf_tunnel_name":               "tex-share",
	"cf_hostname":                  "",
	"backup_schedule":              "off",
	"backup_hour":                  "3",
	"backup_retention_count":       "14",
	"backup_path":                  "",
	"backup_encrypt_auto":          "1",
	"maintenance_mode":             "0",
	"blocked_extensions":           `["exe","msi","dll","com","bat","cmd","ps1","psm1","scr","cpl","vbs","vbe","jse","wsf","wsh","hta","jar","sh","bash","zsh","app","deb","rpm","apk"]`,
	"blocked_mime":                 `["application/x-msdownload","application/x-dosexec","application/x-msi","application/x-sh","application/x-bat","application/x-executable","application/vnd.microsoft.portable-executable","application/x-mach-binary","application/x-elf","text/x-shellscript"]`,
	"require_domain_verification":  "0",
	"monitoring_refresh_seconds":   "5",
	"allow_2fa":                    "1",
	"force_https_links":            "1",
	"public_base_url":              "",
	"default_share_retention_days": "0",
}

// Store caches settings for fast request handling.
type Store struct {
	mu   sync.RWMutex
	db   *sql.DB
	data map[string]string
}

// New loads settings and seeds defaults. Unknown keys are preserved.
func New(db *sql.DB) (*Store, error) {
	s := &Store{db: db, data: map[string]string{}}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	for k, v := range Defaults {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO NOTHING`, k, v); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("seed setting %s: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		s.data[k] = v
	}
	return s, rows.Err()
}

// Get returns a setting or its default.
func (s *Store) Get(key string) string {
	s.mu.RLock()
	v, ok := s.data[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	return Defaults[key]
}

// String is an alias for Get.
func (s *Store) String(key string) string { return s.Get(key) }

// Bool interprets a setting as boolean.
func (s *Store) Bool(key string) bool { return strings.EqualFold(s.Get(key), "true") || s.Get(key) == "1" }

// Int interprets a setting as int64.
func (s *Store) Int(key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s.Get(key)), 10, 64)
	if err != nil {
		v, _ = strconv.ParseInt(Defaults[key], 10, 64)
	}
	return v
}

// Set updates one value in the database and cache.
func (s *Store) Set(key, value string) error {
	if _, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
		return err
	}
	s.mu.Lock()
	s.data[key] = value
	s.mu.Unlock()
	return nil
}

// SetMany updates multiple values atomically.
func (s *Store) SetMany(values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for k, v := range values {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.mu.Lock()
	for k, v := range values {
		s.data[k] = v
	}
	s.mu.Unlock()
	return nil
}

// All returns a copy of all settings.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}
