package app

import (
	"archive/zip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/filetype"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func shareCookieName(shareID string) string {
	return shareCookiePrefix + strings.ReplaceAll(shareID, "-", "")
}

func (a *App) grantShareAccess(w http.ResponseWriter, r *http.Request, share *Share) {
	token := util.RandomToken(32)
	hours := a.settings.Int("share_access_hours")
	if hours < 1 {
		hours = 12
	}
	now := util.Now()
	_, _ = a.db.Exec(`INSERT INTO share_access(token_hash, share_id, user_agent, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
		util.HashToken(token), share.ID, truncate(r.UserAgent(), 256), now, now+hours*3600)
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookieName(share.ID),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) shareAccessGranted(r *http.Request, share *Share) bool {
	if share.PasswordHash == "" {
		return true
	}
	c, err := r.Cookie(shareCookieName(share.ID))
	if err != nil || c.Value == "" {
		return false
	}
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM share_access WHERE token_hash = ? AND share_id = ? AND expires_at > ?`,
		util.HashToken(c.Value), share.ID, util.Now()).Scan(&n)
	return n > 0
}

func (a *App) revokeShareAccess(shareID string) {
	_, _ = a.db.Exec(`DELETE FROM share_access WHERE share_id = ?`, shareID)
}

// loadPublicShare resolves and validates a token for the current host.
func (a *App) loadPublicShare(r *http.Request) (*Share, *Domain, bool) {
	token := r.PathValue("token")
	if token == "" || len(token) > 128 {
		return nil, nil, false
	}
	s, err := a.shareByToken(token)
	if err != nil {
		return nil, nil, false
	}
	d, err := a.domainByID(s.DomainID)
	if err != nil || !a.validShareHost(r, d) {
		return nil, nil, false
	}
	return s, d, true
}

func (a *App) publicShareHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	access := a.shareAccessGranted(r, s)
	payload := a.publicSharePayload(s, access)
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) publicSharePayload(s *Share, access bool) map[string]any {
	eff := effectiveStatus(s)
	available := eff == "active" || eff == "quarantined"
	payload := map[string]any{
		"id": s.ID, "type": s.Type, "isE2ee": s.IsE2EE, "e2eeMode": s.E2EEMode,
		"available": available, "state": eff, "requiresPassword": s.PasswordHash != "",
		"accessGranted": access, "createdAt": s.CreatedAt, "expiresAt": s.ExpiresAt,
		"downloadCount": s.DownloadCount, "downloadLimit": s.DownloadLimit, "viewCount": s.ViewCount,
		"domainHostname": s.DomainHostname, "slug": s.Slug, "namespace": s.Namespace,
	}
	if s.DownloadLimit > 0 {
		remaining := s.DownloadLimit - s.DownloadCount
		if remaining < 0 {
			remaining = 0
		}
		payload["downloadsRemaining"] = remaining
	}
	if !available {
		return payload
	}
	// Metadata that may be shown before the server password is entered.
	if !s.IsE2EE {
		payload["title"] = s.Title
		payload["description"] = s.Description
	}
	if s.PasswordHash != "" && !access {
		return payload
	}
	files, _ := a.shareFiles(s.ID)
	if s.IsE2EE {
		payload["e2eeManifest"] = base64.StdEncoding.EncodeToString(s.E2EEManifest)
		payload["e2eeKeyWrap"] = s.E2EEKeyWrap
		payload["e2eeRecoveryWrap"] = s.E2EERecoveryWrap
		payload["files"] = files
	} else {
		payload["title"] = s.Title
		payload["description"] = s.Description
		payload["textContent"] = s.TextContent
		payload["linkUrl"] = s.LinkURL
		payload["files"] = files
	}
	payload["downloadAllowed"] = a.shareDownloadsAllowed(s)
	payload["zipAvailable"] = !s.IsE2EE && len(files) > 0 && a.shareDownloadsAllowed(s)
	return payload
}

func (a *App) shareDownloadsAllowed(s *Share) bool {
	if effectiveStatus(s) != "active" {
		return false
	}
	if s.Quarantine {
		return false
	}
	if s.DownloadLimit > 0 && s.DownloadCount >= s.DownloadLimit {
		return false
	}
	return true
}

func (a *App) unlockShareHandler(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "")
		return
	}
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if effectiveStatus(s) != "active" && effectiveStatus(s) != "quarantined" {
		writeError(w, http.StatusGone, "share_unavailable", "")
		return
	}
	if s.PasswordHash == "" {
		writeJSON(w, http.StatusOK, a.publicSharePayload(s, true))
		return
	}
	ip := util.ClientIP(r, a.cfg.TrustProxy)
	if !a.loginLimiter.Allow("unlock:"+ip+":"+s.ID, 8, 8) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if len(req.Password) > 256 || !verifyHash(req.Password, s.PasswordHash) {
		writeError(w, http.StatusForbidden, "wrong_password", "")
		return
	}
	a.grantShareAccess(w, r, s)
	writeJSON(w, http.StatusOK, a.publicSharePayload(s, true))
}

func verifyHash(password, hash string) bool {
	return auth.VerifyPassword(password, hash)
}

func (a *App) viewShareHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if !a.shareAccessGranted(r, s) {
		writeError(w, http.StatusForbidden, "password_required", "")
		return
	}
	if effectiveStatus(s) != "active" {
		writeError(w, http.StatusGone, "share_unavailable", "")
		return
	}
	ip := util.ClientIP(r, a.cfg.TrustProxy)
	if !a.apiLimiter.Allow("view:"+ip+":"+s.ID, 2, 2) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	now := util.Now()
	_, _ = a.db.Exec(`UPDATE shares SET view_count = view_count + 1, last_accessed_at = ? WHERE id = ?`, now, s.ID)
	_, _ = a.db.Exec(`INSERT INTO stats_daily(share_id, day, views) VALUES(?, ?, 1)
		ON CONFLICT(share_id, day) DO UPDATE SET views = views + 1`, s.ID, util.Day(now))
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) reserveDownload(s *Share) bool {
	res, err := a.db.Exec(`UPDATE shares SET download_count = download_count + 1
		WHERE id = ? AND status = 'active' AND admin_locked = 0 AND suspended_block = 0
		AND (download_limit = 0 OR download_count < download_limit)`, s.ID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func (a *App) releaseDownload(shareID string) {
	_, _ = a.db.Exec(`UPDATE shares SET download_count = CASE WHEN download_count > 0 THEN download_count - 1 ELSE 0 END WHERE id = ?`, shareID)
}

func (a *App) finishDownload(s *Share, bytes int64) {
	now := util.Now()
	_, _ = a.db.Exec(`UPDATE shares SET bytes_served = bytes_served + ?, last_accessed_at = ? WHERE id = ?`, bytes, now, s.ID)
	_, _ = a.db.Exec(`INSERT INTO stats_daily(share_id, day, downloads, bytes) VALUES(?, ?, 1, ?)
		ON CONFLICT(share_id, day) DO UPDATE SET downloads = downloads + 1, bytes = bytes + excluded.bytes`, s.ID, util.Day(now), bytes)
	a.activity(s.OwnerID, s.ID, "download", map[string]any{"bytes": bytes})
}

func (a *App) shareFileDownloadHandler(inline bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _, ok := a.loadPublicShare(r)
		if !ok {
			writeError(w, http.StatusNotFound, "share_not_found", "")
			return
		}
		if !a.shareAccessGranted(r, s) {
			writeError(w, http.StatusForbidden, "password_required", "")
			return
		}
		if s.IsE2EE {
			writeError(w, http.StatusNotFound, "encrypted_share", "")
			return
		}
		eff := effectiveStatus(s)
		if eff != "active" {
			writeError(w, http.StatusGone, "share_unavailable", "")
			return
		}
		f, err := a.shareFile(s.ID, r.PathValue("fileId"))
		if err != nil {
			writeError(w, http.StatusNotFound, "file_not_found", "")
			return
		}
		if !fileReady(a, f) {
			writeError(w, http.StatusLocked, "file_not_ready", "")
			return
		}
		if inline && !filetype.InlineAllowed(f.Mime) {
			writeError(w, http.StatusForbidden, "preview_not_allowed", "")
			return
		}
		if !inline && !a.shareDownloadsAllowed(s) {
			writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
			return
		}
		release, ok := a.gates.TryAcquire("dl:"+s.ID, int(a.settings.Int("download_concurrency_per_user")), int(a.settings.Int("global_download_limit")))
		if !ok {
			writeError(w, http.StatusTooManyRequests, "download_limit", "")
			return
		}
		defer release()
		rangeReq := r.Header.Get("Range") != ""
		counted := false
		if !inline && !rangeReq && r.Method == http.MethodGet {
			if !a.reserveDownload(s) {
				writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
				return
			}
			counted = true
		}
		file, err := a.store.Open(f.StoragePath)
		if err != nil {
			if counted {
				a.releaseDownload(s.ID)
			}
			writeError(w, http.StatusNotFound, "file_not_found", "")
			return
		}
		defer file.Close()
		disposition := "attachment"
		if inline {
			disposition = "inline"
		}
		w.Header().Set("Content-Type", f.Mime)
		w.Header().Set("Content-Disposition", util.ContentDisposition(disposition, f.Name))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Accept-Ranges", "bytes")
		if f.SHA256 != "" {
			w.Header().Set("ETag", `"`+f.SHA256+`"`)
		}
		tw := &trackingWriter{ResponseWriter: w}
		http.ServeContent(tw, r, f.Name, time.Unix(f.CreatedAt, 0), file)
		if counted {
			if tw.err != nil || tw.n != f.SizeBytes {
				a.releaseDownload(s.ID)
			} else {
				a.finishDownload(s, f.SizeBytes)
			}
		}
	}
}

type trackingWriter struct {
	http.ResponseWriter
	n   int64
	err error
}

func (t *trackingWriter) Write(p []byte) (int, error) {
	n, err := t.ResponseWriter.Write(p)
	t.n += int64(n)
	if err != nil {
		t.err = err
	}
	return n, err
}

func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

func fileReady(a *App, f *ShareFile) bool {
	if f.ScanStatus == "infected" || f.ScanStatus == "pending" {
		return false
	}
	if a.clamavEnabled() && f.ScanStatus != "clean" && f.ScanStatus != "encrypted" {
		return false
	}
	return true
}

// ownerE2EEBlobHandler streams the encrypted container for an owner so the
// browser can decrypt and re-encrypt files during key rotation. It requires an
// authenticated owner session and does not consume a download.
func (a *App) ownerE2EEBlobHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if !s.IsE2EE {
		writeError(w, http.StatusBadRequest, "not_encrypted", "")
		return
	}
	f, err := a.shareFile(s.ID, r.PathValue("fileId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	file, err := a.store.Open(f.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", f.ID+".bin"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, f.ID+".bin", time.Unix(f.CreatedAt, 0), file)
}

func (a *App) e2eeBlobHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if !s.IsE2EE {
		writeError(w, http.StatusNotFound, "not_encrypted", "")
		return
	}
	if !a.shareAccessGranted(r, s) {
		writeError(w, http.StatusForbidden, "password_required", "")
		return
	}
	if effectiveStatus(s) != "active" {
		writeError(w, http.StatusGone, "share_unavailable", "")
		return
	}
	f, err := a.shareFile(s.ID, r.PathValue("fileId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	if !a.shareDownloadsAllowed(s) {
		writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
		return
	}
	release, ok := a.gates.TryAcquire("dl:"+s.ID, int(a.settings.Int("download_concurrency_per_user")), int(a.settings.Int("global_download_limit")))
	if !ok {
		writeError(w, http.StatusTooManyRequests, "download_limit", "")
		return
	}
	defer release()
	rangeReq := r.Header.Get("Range") != ""
	counted := false
	if !rangeReq && r.Method == http.MethodGet {
		if !a.reserveDownload(s) {
			writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
			return
		}
		counted = true
	}
	file, err := a.store.Open(f.StoragePath)
	if err != nil {
		if counted {
			a.releaseDownload(s.ID)
		}
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", f.ID+".bin"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Accept-Ranges", "bytes")
	tw := &trackingWriter{ResponseWriter: w}
	http.ServeContent(tw, r, f.ID+".bin", time.Unix(f.CreatedAt, 0), file)
	if counted {
		if tw.err != nil || tw.n != f.EncryptedSizeBytes {
			a.releaseDownload(s.ID)
		} else {
			a.finishDownload(s, f.EncryptedSizeBytes)
		}
	}
}

func (a *App) zipDownloadHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if !a.shareAccessGranted(r, s) {
		writeError(w, http.StatusForbidden, "password_required", "")
		return
	}
	if s.IsE2EE {
		writeError(w, http.StatusBadRequest, "e2ee_client_zip", "")
		return
	}
	if effectiveStatus(s) != "active" {
		writeError(w, http.StatusGone, "share_unavailable", "")
		return
	}
	files, err := a.shareFiles(s.ID)
	if err != nil || len(files) == 0 {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	for _, f := range files {
		if !fileReady(a, &f) {
			writeError(w, http.StatusLocked, "file_not_ready", "")
			return
		}
	}
	if !a.shareDownloadsAllowed(s) {
		writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
		return
	}
	zipRelease, ok := a.zipGate.TryAcquire("zip", int(a.settings.Int("zip_concurrency")), int(a.settings.Int("zip_concurrency")))
	if !ok {
		writeError(w, http.StatusTooManyRequests, "zip_limit", "")
		return
	}
	defer zipRelease()
	release, ok := a.gates.TryAcquire("dl:"+s.ID, int(a.settings.Int("download_concurrency_per_user")), int(a.settings.Int("global_download_limit")))
	if !ok {
		writeError(w, http.StatusTooManyRequests, "download_limit", "")
		return
	}
	defer release()
	if !a.reserveDownload(s) {
		writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
		return
	}
	success := false
	defer func() {
		if !success {
			a.releaseDownload(s.ID)
		}
	}()
	name := s.Slug
	if name == "" {
		name = "share"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", name+".zip"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	tw := &trackingWriter{ResponseWriter: w}
	zw := zip.NewWriter(tw)
	used := map[string]bool{}
	var total int64
	for _, f := range files {
		file, err := a.store.Open(f.StoragePath)
		if err != nil {
			return
		}
		header := &zip.FileHeader{Name: uniqueZipName(used, f.Name), Method: zip.Store, Modified: time.Unix(f.CreatedAt, 0)}
		header.SetMode(0o600)
		entry, err := zw.CreateHeader(header)
		if err != nil {
			file.Close()
			return
		}
		n, err := io.Copy(entry, file)
		file.Close()
		if err != nil {
			return
		}
		total += n
	}
	if err := zw.Close(); err != nil {
		return
	}
	success = true
	if tw.n > 0 {
		total = tw.n
	}
	a.finishDownload(s, total)
}

func uniqueZipName(used map[string]bool, name string) string {
	name = cleanShareName(name)
	if name == "" {
		name = "file"
	}
	if !used[name] {
		used[name] = true
		return name
	}
	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		base, ext = name[:i], name[i:]
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func (a *App) shareStatsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := r.PathValue("id")
	s, err := a.shareByID(id)
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	days := int(util.ParseInt(r.URL.Query().Get("days"), 30))
	if days < 1 || days > 365 {
		days = 30
	}
	rows, err := a.db.Query(`SELECT day, views, downloads, bytes FROM stats_daily WHERE share_id = ? AND day >= ? ORDER BY day ASC`,
		id, time.Now().AddDate(0, 0, -days).UTC().Format("2006-01-02"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var day string
		var views, downloads, bytes int64
		if rows.Scan(&day, &views, &downloads, &bytes) == nil {
			out = append(out, map[string]any{"day": day, "views": views, "downloads": downloads, "bytes": bytes})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareSummary(s), "daily": out})
}

// E2EE ZIP tickets: the server cannot build the archive, so it reserves a
// download when the client starts and finalises when the client confirms.

type zipTicket struct {
	ShareID string `json:"s"`
	Expires int64  `json:"e"`
	Nonce   string `json:"n"`
}

func (a *App) signTicket(shareID string) (string, error) {
	nonce := util.RandomHex(16)
	expires := util.Now() + 3600
	if _, err := a.db.Exec(`INSERT INTO zip_tickets(nonce, share_id, created_at, expires_at) VALUES(?, ?, ?, ?)`,
		nonce, shareID, util.Now(), expires); err != nil {
		return "", err
	}
	payload := util.JSON(zipTicket{ShareID: shareID, Expires: expires, Nonce: nonce})
	mac := hmac.New(sha256.New, a.secrets.HMACKey("zip-ticket"))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// consumeTicket verifies a signed ticket and deletes its nonce in one atomic
// statement, so a ticket can only be completed or cancelled once.
func (a *App) consumeTicket(token, shareID string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.secrets.HMACKey("zip-ticket"))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return false
	}
	var t zipTicket
	if json.Unmarshal(payload, &t) != nil || t.ShareID != shareID || t.Expires < util.Now() || t.Nonce == "" {
		return false
	}
	res, err := a.db.Exec(`DELETE FROM zip_tickets WHERE nonce = ? AND share_id = ? AND expires_at > ?`,
		t.Nonce, shareID, util.Now())
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func (a *App) e2eeZipStartHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if !s.IsE2EE || !a.shareAccessGranted(r, s) {
		writeError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	if !a.shareDownloadsAllowed(s) {
		writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
		return
	}
	if !a.reserveDownload(s) {
		writeError(w, http.StatusTooManyRequests, "download_limit_reached", "")
		return
	}
	ticket, err := a.signTicket(s.ID)
	if err != nil {
		a.releaseDownload(s.ID)
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": ticket})
}

func (a *App) e2eeZipCompleteHandler(w http.ResponseWriter, r *http.Request) {
	s, _, ok := a.loadPublicShare(r)
	if !ok {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	var req struct {
		Ticket string `json:"ticket"`
		Bytes  int64  `json:"bytes"`
		Cancel bool   `json:"cancel"`
	}
	if err := readJSON(r, &req); err != nil || !a.consumeTicket(req.Ticket, s.ID) {
		writeError(w, http.StatusBadRequest, "invalid_ticket", "")
		return
	}
	if req.Cancel {
		a.releaseDownload(s.ID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	bytes := req.Bytes
	if bytes < 0 {
		bytes = 0
	}
	if bytes > s.SizeBytes {
		bytes = s.SizeBytes
	}
	a.finishDownload(s, bytes)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}


