package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

type settingKind struct {
	kind string
	min  int64
	max  int64
	enum []string
	maxLen int
}

var editableSettings = map[string]settingKind{
	"app_name":                      {kind: "string", maxLen: 60},
	"default_storage_limit_bytes":   {kind: "int", min: 0, max: 1 << 50},
	"allow_unlimited_storage":       {kind: "bool"},
	"global_storage_limit_bytes":    {kind: "int", min: 0, max: 1 << 50},
	"max_file_size_bytes":           {kind: "int", min: 1 << 20, max: 1 << 42},
	"max_share_size_bytes":          {kind: "int", min: 1 << 20, max: 1 << 43},
	"resumable_threshold_bytes":     {kind: "int", min: 1 << 20, max: 1 << 40},
	"trash_retention_days":          {kind: "int", min: 0, max: 3650},
	"stats_retention_days":          {kind: "int", min: 1, max: 3650},
	"max_session_hours":             {kind: "int", min: 1, max: 720},
	"remember_me_days":              {kind: "int", min: 1, max: 365},
	"share_access_hours":            {kind: "int", min: 1, max: 168},
	"password_min_length":           {kind: "int", min: 8, max: 128},
	"login_rate_per_minute":         {kind: "int", min: 1, max: 1000},
	"login_failures_before_lock":    {kind: "int", min: 1, max: 100},
	"lockout_minutes":               {kind: "int", min: 1, max: 1440},
	"api_rate_per_minute":           {kind: "int", min: 1, max: 100000},
	"upload_concurrency_per_user":   {kind: "int", min: 1, max: 50},
	"download_concurrency_per_user": {kind: "int", min: 1, max: 100},
	"global_upload_limit":           {kind: "int", min: 1, max: 1000},
	"global_download_limit":         {kind: "int", min: 1, max: 5000},
	"zip_concurrency":               {kind: "int", min: 1, max: 20},
	"twofa_attempts":                {kind: "int", min: 1, max: 20},
	"upload_queue_limit":            {kind: "int", min: 1, max: 100},
	"antivirus_enabled":             {kind: "bool"},
	"freshclam_interval_hours":      {kind: "int", min: 1, max: 168},
	"cloudflare_enabled":            {kind: "bool"},
	"cf_tunnel_name":                {kind: "string", maxLen: 64},
	"cf_hostname":                   {kind: "string", maxLen: 253},
	"backup_schedule":               {kind: "enum", enum: []string{"off", "daily", "weekly"}},
	"backup_hour":                   {kind: "int", min: 0, max: 23},
	"backup_retention_count":        {kind: "int", min: 1, max: 365},
	"backup_path":                   {kind: "string", maxLen: 512},
	"backup_encrypt_auto":           {kind: "bool"},
	"maintenance_mode":              {kind: "bool"},
	"blocked_extensions":            {kind: "stringlist"},
	"blocked_mime":                  {kind: "stringlist"},
	"require_domain_verification":   {kind: "bool"},
	"monitoring_refresh_seconds":    {kind: "int", min: 1, max: 60},
	"allow_2fa":                     {kind: "bool"},
	"force_https_links":             {kind: "bool"},
	"public_base_url":               {kind: "string", maxLen: 255},
	"default_share_retention_days":  {kind: "int", min: 0, max: 3650},

	// Flexible DNS / network / reverse-proxy management.
	"network_dns_provider":   {kind: "enum", enum: []string{"manual", "cloudflare", "none"}},
	"network_connection":     {kind: "enum", enum: []string{"direct", "proxy", "tunnel", "static", "ddns"}},
	"network_proxy_kind":     {kind: "enum", enum: []string{"none", "nginx", "caddy", "traefik", "npm", "haproxy", "other"}},
	"network_proxy_hostname": {kind: "string", maxLen: 253},
	"network_proxy_upstream": {kind: "string", maxLen: 253},
	"network_admin_exposed":  {kind: "bool"},
	"trusted_proxies":        {kind: "cidrlist"},
	"cf_account_id":          {kind: "string", maxLen: 64},
	"cf_zone_id":             {kind: "string", maxLen: 64},
	"cf_zone_name":           {kind: "string", maxLen: 253},
	"ddns_enabled":           {kind: "bool"},
	"ddns_provider":          {kind: "enum", enum: []string{"cloudflare", "manual"}},
	"ddns_interval_minutes":  {kind: "int", min: 1, max: 1440},
	"ddns_ipv4":              {kind: "bool"},
	"ddns_ipv6":              {kind: "bool"},
	"public_ipv4":            {kind: "ip"},
	"public_ipv6":            {kind: "ip"},
}

func (a *App) validateSetting(key, value string) (string, error) {
	spec, ok := editableSettings[key]
	if !ok {
		return "", fmt.Errorf("unknown setting %q", key)
	}
	value = strings.TrimSpace(value)
	switch spec.kind {
	case "bool":
		if value != "0" && value != "1" {
			return "", fmt.Errorf("%s must be 0 or 1", key)
		}
	case "int":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < spec.min || n > spec.max {
			return "", fmt.Errorf("%s is out of range", key)
		}
		value = strconv.FormatInt(n, 10)
	case "enum":
		valid := false
		for _, e := range spec.enum {
			if value == e {
				valid = true
				break
			}
		}
		if !valid {
			return "", fmt.Errorf("%s has an invalid value", key)
		}
	case "string":
		if spec.maxLen > 0 && len(value) > spec.maxLen {
			return "", fmt.Errorf("%s is too long", key)
		}
		if key == "public_base_url" && value != "" {
			if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
				return "", fmt.Errorf("%s must be an http(s) URL", key)
			}
		}
		if key == "cf_tunnel_name" && value != "" && !util.ValidSlug(value) {
			return "", fmt.Errorf("%s must be a slug", key)
		}
		if key == "cf_hostname" && value != "" && !util.ValidHostname(value) {
			return "", fmt.Errorf("%s must be a hostname", key)
		}
		if key == "network_proxy_hostname" && value != "" && !util.ValidHostname(value) {
			return "", fmt.Errorf("%s must be a hostname", key)
		}
		if key == "cf_zone_name" && value != "" && !util.ValidHostname(value) {
			return "", fmt.Errorf("%s must be a hostname", key)
		}
		if key == "network_proxy_upstream" && value != "" && net.ParseIP(value) == nil && !util.ValidHostname(value) {
			return "", fmt.Errorf("%s must be an IP or hostname", key)
		}
	case "stringlist":
		var list []string
		if err := json.Unmarshal([]byte(value), &list); err != nil {
			return "", fmt.Errorf("%s must be a JSON string array", key)
		}
		if len(list) > 200 {
			return "", fmt.Errorf("%s has too many entries", key)
		}
		for _, item := range list {
			if len(item) > 128 {
				return "", fmt.Errorf("%s contains an overly long entry", key)
			}
		}
	case "cidrlist":
		// Comma separated IPs / CIDRs of trusted proxies. Invalid entries are
		// rejected instead of silently ignored so a typo cannot weaken header
		// handling.
		for _, raw := range strings.Split(value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if len(raw) > 64 {
				return "", fmt.Errorf("%s contains an overly long entry", key)
			}
			if strings.Contains(raw, "/") {
				if _, _, err := net.ParseCIDR(raw); err != nil {
					return "", fmt.Errorf("%s contains an invalid CIDR %q", key, raw)
				}
				continue
			}
			if net.ParseIP(raw) == nil {
				return "", fmt.Errorf("%s contains an invalid address %q", key, raw)
			}
		}
	case "ip":
		// Empty is allowed (clears the value); otherwise it must parse.
		if value != "" && net.ParseIP(value) == nil {
			return "", fmt.Errorf("%s must be an IP address", key)
		}
	}
	return value, nil
}

func (a *App) adminSettingsHandler(w http.ResponseWriter, r *http.Request) {
	all := a.settings.All()
	for _, k := range []string{"cf_api_token_enc", "cf_tunnel_token_enc", "setup_completed", "logo_path", "favicon_path"} {
		delete(all, k)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": all,
		"backup":   a.backupSettings(),
		"system":   a.systemInfo(),
		"branding": a.brandingInfo(),
	})
}

func (a *App) adminPatchSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings map[string]string `json:"settings"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if len(req.Settings) > 100 {
		writeError(w, http.StatusBadRequest, "too_many_settings", "")
		return
	}
	clean := map[string]string{}
	for k, v := range req.Settings {
		valid, err := a.validateSetting(k, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_setting", err.Error())
			return
		}
		clean[k] = valid
	}
	if err := a.settings.SetMany(clean); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.refreshTrustedProxies()
	a.refreshHostCache()
	a.auditRequest(r, "settings.update", "settings", "", "success", map[string]any{"keys": sortedKeys(clean)})
	if _, ok := clean["backup_retention_count"]; ok {
		a.pruneBackups()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": clean})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (a *App) systemInfo() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	uptime := int64(time.Since(a.startedAt).Seconds())
	dbSize := int64(0)
	if fi, err := os.Stat(filepath.Join(a.cfg.DataDir, "db", "texshare.db")); err == nil {
		dbSize = fi.Size()
	}
	return map[string]any{
		"version": a.cfg.Version, "commit": a.cfg.Commit, "go": runtime.Version(),
		"os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(),
		"startedAt": a.startedAt.Unix(), "uptimeSeconds": uptime,
		"heapBytes": ms.HeapAlloc, "sysBytes": ms.Sys, "goroutines": runtime.NumGoroutine(),
		"dbSizeBytes": dbSize, "errors": atomic.LoadInt64(&a.errorCount),
		"publicUrl": a.cfg.PublicURL, "adminUrl": a.cfg.AdminURL,
	}
}

func (a *App) monitoringHandler(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	total, free, diskErr := diskSpace(a.cfg.DataDir)
	var dbSize int64
	if fi, err := os.Stat(filepath.Join(a.cfg.DataDir, "db", "texshare.db")); err == nil {
		dbSize = fi.Size()
	}
	stats := map[string]any{
		"uptimeSeconds": int64(time.Since(a.startedAt).Seconds()),
		"goroutines":    runtime.NumGoroutine(),
		"heapBytes":     ms.HeapAlloc,
		"heapSysBytes":  ms.HeapSys,
		"stackBytes":    ms.StackInuse,
		"gcCycles":      ms.NumGC,
		"cpus":          runtime.NumCPU(),
		"dbSizeBytes":   dbSize,
		"errors":        atomic.LoadInt64(&a.errorCount),
		"activeUploads": atomic.LoadInt64(&a.activeUploads),
		"activeZips":    atomic.LoadInt64(&a.activeZips),
	}
	if diskErr == nil {
		stats["diskTotalBytes"] = total
		stats["diskFreeBytes"] = free
		stats["diskUsedBytes"] = total - free
	}
	var users, shares, files, activeShares int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&shares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE status = 'active'`).Scan(&activeShares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM share_files`).Scan(&files)
	var storageUsed, storageReserved int64
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(storage_used_bytes),0), COALESCE(SUM(storage_reserved_bytes),0) FROM users`).Scan(&storageUsed, &storageReserved)
	stats["counts"] = map[string]any{"users": users, "shares": shares, "activeShares": activeShares, "files": files,
		"storageUsedBytes": storageUsed, "storageReservedBytes": storageReserved}
	if a.clamav != nil {
		stats["clamav"] = a.clamav.Status()
	}
	if a.tunnel != nil {
		stats["tunnel"] = a.tunnel.Status()
	}
	if a.cloudflare != nil {
		stats["cloudflare"] = a.cloudflare.Status()
	}
	if a.network != nil && a.network.ddns != nil {
		stats["ddns"] = a.network.ddns.Status()
	}
	stats["network"] = map[string]any{
		"dnsProvider":    a.globalDNSProvider(),
		"connection":     a.globalNetworkMode(),
		"proxyKind":      a.globalProxyKind(),
		"trustedProxies": a.trusted().Strings(),
	}
	writeJSON(w, http.StatusOK, map[string]any{"monitoring": stats, "system": a.systemInfo()})
}

func (a *App) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	now := util.Now()
	dayAgo := now - 86400
	var users, suspended, admins, shares, activeShares, deletedShares, expiredShares, domains, files, pendingScans, backups int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_suspended = 1`).Scan(&suspended)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&shares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE status = 'active'`).Scan(&activeShares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE status = 'deleted'`).Scan(&deletedShares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE status = 'expired'`).Scan(&expiredShares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&domains)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM share_files`).Scan(&files)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM share_files WHERE scan_status IN ('pending','scanning')`).Scan(&pendingScans)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backups)
	var downloads24, views24, bytes24 int64
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(downloads),0), COALESCE(SUM(views),0), COALESCE(SUM(bytes),0) FROM stats_daily WHERE day >= ?`, util.Day(dayAgo)).
		Scan(&downloads24, &views24, &bytes24)
	var storageUsed, storageReserved int64
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(storage_used_bytes),0), COALESCE(SUM(storage_reserved_bytes),0) FROM users`).Scan(&storageUsed, &storageReserved)

	type auditItem struct {
		ID       int64  `json:"id"`
		TS       int64  `json:"ts"`
		Actor    string `json:"actor"`
		Action   string `json:"action"`
		Object   string `json:"objectType"`
		ObjectID string `json:"objectId"`
		Result   string `json:"result"`
	}
	recent := []auditItem{}
	rows, err := a.db.Query(`SELECT id, ts, actor_username, action, object_type, object_id, result FROM audit_log ORDER BY id DESC LIMIT 12`)
	if err == nil {
		for rows.Next() {
			var it auditItem
			if rows.Scan(&it.ID, &it.TS, &it.Actor, &it.Action, &it.Object, &it.ObjectID, &it.Result) == nil {
				recent = append(recent, it)
			}
		}
		rows.Close()
	}
	var newUsers int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE created_at >= ?`, dayAgo).Scan(&newUsers)
	var newShares int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE created_at >= ?`, dayAgo).Scan(&newShares)

	writeJSON(w, http.StatusOK, map[string]any{
		"counts": map[string]any{
			"users": users, "suspendedUsers": suspended, "admins": admins, "shares": shares,
			"activeShares": activeShares, "trashedShares": deletedShares, "expiredShares": expiredShares,
			"domains": domains, "files": files, "pendingScans": pendingScans, "backups": backups,
			"newUsers24h": newUsers, "newShares24h": newShares,
		},
		"usage": map[string]any{
			"storageUsedBytes": storageUsed, "storageReservedBytes": storageReserved,
			"downloads24h": downloads24, "views24h": views24, "bytesServed24h": bytes24,
		},
		"recentAudit": recent,
		"system":      a.systemInfo(),
		"monitoring":  map[string]any{"tunnel": tunnelStatus(a), "clamav": clamavStatus(a)},
	})
}

func tunnelStatus(a *App) any {
	if a.tunnel == nil {
		return nil
	}
	return a.tunnel.Status()
}

func clamavStatus(a *App) any {
	if a.clamav == nil {
		return nil
	}
	return a.clamav.Status()
}

func (a *App) auditListHandler(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePage(r)
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []any{}
	if v := strings.TrimSpace(q.Get("action")); v != "" {
		where = append(where, "action LIKE ?")
		args = append(args, v+"%")
	}
	if v := strings.TrimSpace(q.Get("actor")); v != "" {
		where = append(where, "actor_username LIKE ?")
		args = append(args, v+"%")
	}
	if v := strings.TrimSpace(q.Get("result")); v != "" {
		where = append(where, "result = ?")
		args = append(args, v)
	}
	if v := util.ParseInt(q.Get("from"), 0); v > 0 {
		where = append(where, "ts >= ?")
		args = append(args, v)
	}
	if v := util.ParseInt(q.Get("to"), 0); v > 0 {
		where = append(where, "ts <= ?")
		args = append(args, v)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE `+clause, args...).Scan(&total)
	args = append(args, limit, (page-1)*limit)
	rows, err := a.db.Query(`SELECT id, ts, actor_id, actor_username, action, object_type, object_id, result, details, prev_hash, entry_hash
		FROM audit_log WHERE `+clause+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, ts, actorID int64
		var actor, action, objType, objID, result, details, prevHash, entryHash string
		if rows.Scan(&id, &ts, &actorID, &actor, &action, &objType, &objID, &result, &details, &prevHash, &entryHash) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "ts": ts, "actorId": actorID, "actor": actor, "action": action,
			"objectType": objType, "objectId": objID, "result": result,
			"details": json.RawMessage(details), "prevHash": prevHash, "entryHash": entryHash,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "total": total, "page": page, "limit": limit})
}

func (a *App) auditVerifyHandler(w http.ResponseWriter, r *http.Request) {
	res, err := a.verifyAuditChain()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *App) auditExportHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, ts, actor_username, action, object_type, object_id, result, details, entry_hash FROM audit_log ORDER BY id ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", "texshare-audit.csv"))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "id,timestamp,actor,action,object_type,object_id,result,details,entry_hash\n")
	for rows.Next() {
		var id, ts int64
		var actor, action, objType, objID, result, details, hash string
		if rows.Scan(&id, &ts, &actor, &action, &objType, &objID, &result, &details, &hash) != nil {
			continue
		}
		line := fmt.Sprintf("%d,%s,%s,%s,%s,%s,%s,%s,%s\n", id, time.Unix(ts, 0).UTC().Format(time.RFC3339),
			csvEscape(actor), csvEscape(action), csvEscape(objType), csvEscape(objID), csvEscape(result), csvEscape(details), hash)
		_, _ = io.WriteString(w, line)
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func (a *App) brandingInfo() map[string]any {
	return map[string]any{
		"appName":   a.settings.Get("app_name"),
		"logo":      a.settings.Get("logo_path") != "",
		"favicon":   a.settings.Get("favicon_path") != "",
		"logoUrl":   "/api/public/branding/logo",
		"faviconUrl": "/api/public/branding/favicon",
	}
}

var brandingKinds = map[string]struct {
	setting string
	max     int64
}{
	"logo":    {setting: "logo_path", max: 4 << 20},
	"favicon": {setting: "favicon_path", max: 512 << 10},
}

func (a *App) adminUploadBrandingHandler(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	spec, ok := brandingKinds[kind]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_kind", "")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required", "")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, spec.max+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload_failed", "")
		return
	}
	if int64(len(data)) > spec.max {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "")
		return
	}
	mime := http.DetectContentType(data)
	ext := ""
	switch mime {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/x-icon", "image/vnd.microsoft.icon":
		ext = ".ico"
	default:
		if len(data) > 4 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 {
			ext = ".ico"
		} else {
			writeError(w, http.StatusBadRequest, "unsupported_image", "")
			return
		}
	}
	dir := filepath.Join(a.cfg.DataDir, "branding")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	name := fmt.Sprintf("%s-%s%s", kind, util.RandomHex(6), ext)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	old := a.settings.Get(spec.setting)
	if err := a.settings.Set(spec.setting, name); err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if old != "" {
		_ = os.Remove(filepath.Join(dir, filepath.Base(old)))
	}
	a.auditRequest(r, "branding.upload", "branding", kind, "success", map[string]any{"bytes": len(data)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "branding": a.brandingInfo()})
}

func (a *App) adminDeleteBrandingHandler(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	spec, ok := brandingKinds[kind]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_kind", "")
		return
	}
	old := a.settings.Get(spec.setting)
	_ = a.settings.Set(spec.setting, "")
	if old != "" {
		_ = os.Remove(filepath.Join(a.cfg.DataDir, "branding", filepath.Base(old)))
	}
	a.auditRequest(r, "branding.delete", "branding", kind, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) publicBrandingHandler(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	spec, ok := brandingKinds[kind]
	if !ok {
		http.NotFound(w, r)
		return
	}
	name := a.settings.Get(spec.setting)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.cfg.DataDir, "branding", filepath.Base(name)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	data, _ := io.ReadAll(io.LimitReader(f, spec.max+1))
	if int64(len(data)) > spec.max {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if fi != nil {
		http.ServeContent(w, r, name, fi.ModTime(), bytes.NewReader(data))
		return
	}
	_, _ = w.Write(data)
}

func (a *App) publicMetaHandler(w http.ResponseWriter, r *http.Request) {
	domainHost := ""
	if d, err := a.defaultDomain(); err == nil {
		domainHost = d.Hostname
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"appName":     a.settings.Get("app_name"),
		"version":     a.cfg.Version,
		"setupNeeded": !a.setupComplete(),
		"maintenance": a.settings.Bool("maintenance_mode"),
		"branding":    a.brandingInfo(),
		"domain":      domainHost,
		"languages":   []string{"de", "en"},
		"allow2fa":    a.settings.Bool("allow_2fa"),
		"registration": false,
	})
}

func (a *App) publicConfigHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	domains, _ := a.allowedDomainsFor(u)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": a.userDTO(u), "domains": domains, "adminUrl": a.cfg.AdminURL,
		"limits": map[string]any{
			"maxFileSizeBytes":  a.settings.Int("max_file_size_bytes"),
			"maxShareSizeBytes": a.settings.Int("max_share_size_bytes"),
			"chunkSize":         uploadChunkSize,
			"resumableThreshold": a.settings.Int("resumable_threshold_bytes"),
			"sharePasswordMin":  8,
			"defaultRetentionDays": a.settings.Int("default_share_retention_days"),
			"storageLimitBytes": u.StorageLimitBytes,
			"storageUsedBytes":  u.StorageUsedBytes,
			"storageReservedBytes": u.StorageReservedBytes,
		},
	})
}

func (a *App) cloudflareStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"cloudflare": a.cloudflare.Status(),
		"tunnel":     a.tunnel.Status(),
	})
}

// cloudflareLoginHandler starts the browser-based `cloudflared tunnel login`
// flow and returns the dashboard URL the administrator must open.
func (a *App) cloudflareLoginHandler(w http.ResponseWriter, r *http.Request) {
	url, err := a.cloudflare.StartLogin()
	if err != nil {
		writeError(w, http.StatusBadRequest, "cloudflare_error", err.Error())
		return
	}
	a.auditRequest(r, "cloudflare.login", "cloudflare", "", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": url})
}

func (a *App) cloudflareCancelHandler(w http.ResponseWriter, r *http.Request) {
	a.cloudflare.CancelLogin()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// cloudflareProvisionHandler creates (or reuses) the tunnel, points the hostname
// at it and writes the cloudflared configuration, then enables the connector.
func (a *App) cloudflareProvisionHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname   string `json:"hostname"`
		TunnelName string `json:"tunnelName"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
	if hostname == "" || !util.ValidHostname(hostname) {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "")
		return
	}
	values := map[string]string{"cf_hostname": hostname}
	if name := strings.TrimSpace(req.TunnelName); name != "" {
		if !util.ValidSlug(name) {
			writeError(w, http.StatusBadRequest, "invalid_tunnel_name", "")
			return
		}
		values["cf_tunnel_name"] = name
	}
	if err := a.settings.SetMany(values); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 90*time.Second)
	defer cancel()
	creds, err := a.cloudflare.EnsureTunnel(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cloudflare_error", err.Error())
		return
	}
	if err := a.cloudflare.RouteDNS(ctx, creds.TunnelName, hostname); err != nil {
		writeError(w, http.StatusBadRequest, "dns_error", err.Error())
		return
	}
	if err := a.cloudflare.WriteConfig(creds, hostname); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := a.settings.SetMany(map[string]string{
		"cloudflare_enabled": "1",
		"cf_tunnel_id":       creds.TunnelID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.auditRequest(r, "cloudflare.provision", "cloudflare", creds.TunnelID, "success", map[string]any{"hostname": hostname})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cloudflare": a.cloudflare.Status(), "tunnel": a.tunnel.Status()})
}

func (a *App) cloudflareDisableHandler(w http.ResponseWriter, r *http.Request) {
	_ = a.settings.Set("cloudflare_enabled", "0")
	a.auditRequest(r, "cloudflare.disable", "cloudflare", "", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// cloudflareDisconnectHandler removes the local certificate, credentials and
// configuration and forgets the tunnel.
func (a *App) cloudflareDisconnectHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 30*time.Second)
	defer cancel()
	if err := a.cloudflare.Disconnect(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	a.auditRequest(r, "cloudflare.disconnect", "cloudflare", "", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
