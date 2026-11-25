// Package config loads all process configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config contains process-level configuration. Runtime-configurable values live
// in the settings table; only bootstrap/secret material belongs here.
type Config struct {
	DataDir       string
	PublicAddr    string
	AdminAddr     string
	PublicURL     string
	AdminURL      string
	AllowedHosts  []string
	AdminHosts    []string
	TrustProxy    bool
	ForceSecure   bool
	LogLevel      string
	AccessLog     bool
	BackupKeyFile string
	ClamAVEnabled bool
	ClamavMode    string // off | managed | external
	ClamavPath    string
	FreshclamPath string
	ClamdscanPath string
	Cloudflared   string
	CloudflareOn  bool
	Dev           bool
	Version       string
	Commit        string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads configuration and creates the directory skeleton.
func Load() (*Config, error) {
	dataDir := env("TEXSHARE_DATA_DIR", "./data")
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg := &Config{
		DataDir:       abs,
		PublicAddr:    env("TEXSHARE_PUBLIC_ADDR", "0.0.0.0:3535"),
		AdminAddr:     env("TEXSHARE_ADMIN_ADDR", "0.0.0.0:3636"),
		PublicURL:     strings.TrimRight(env("TEXSHARE_PUBLIC_URL", ""), "/"),
		AdminURL:      strings.TrimRight(env("TEXSHARE_ADMIN_URL", ""), "/"),
		AllowedHosts:  splitList(env("TEXSHARE_ALLOWED_HOSTS", "")),
		AdminHosts:    splitList(env("TEXSHARE_ADMIN_ALLOWED_HOSTS", "")),
		TrustProxy:    envBool("TEXSHARE_TRUST_PROXY", false),
		ForceSecure:   envBool("TEXSHARE_FORCE_SECURE_COOKIES", false),
		LogLevel:      strings.ToLower(env("TEXSHARE_LOG_LEVEL", "info")),
		AccessLog:     envBool("TEXSHARE_ACCESS_LOG", true),
		BackupKeyFile: env("TEXSHARE_BACKUP_KEY_FILE", ""),
		ClamAVEnabled: envBool("TEXSHARE_CLAMAV_ENABLED", false),
		ClamavMode:    strings.ToLower(env("TEXSHARE_CLAMAV_MODE", "managed")),
		ClamavPath:    env("TEXSHARE_CLAMD_PATH", "clamd"),
		FreshclamPath: env("TEXSHARE_FRESHCLAM_PATH", "freshclam"),
		ClamdscanPath: env("TEXSHARE_CLAMDSCAN_PATH", "clamdscan"),
		Cloudflared:   env("TEXSHARE_CLOUDFLARED_PATH", "cloudflared"),
		CloudflareOn:  envBool("TEXSHARE_CLOUDFLARE_ENABLED", false),
		Dev:           envBool("TEXSHARE_DEV", false),
		Version:       env("TEXSHARE_VERSION", "1.0.0"),
		Commit:        env("TEXSHARE_COMMIT", "dev"),
	}
	for _, p := range []string{
		"", "db", "files", "uploads", "quarantine", "backups", "branding", "clamav", "tmp",
	} {
		if err := os.MkdirAll(filepath.Join(abs, p), 0o750); err != nil {
			return nil, fmt.Errorf("create data directory %q: %w", p, err)
		}
	}
	if cfg.ClamAVEnabled {
		cfg.ClamavMode = "managed"
	}
	return cfg, nil
}

// Timeout used by external integrations.
const ExternalTimeout = 8 * time.Second
