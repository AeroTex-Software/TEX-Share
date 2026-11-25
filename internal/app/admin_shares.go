package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) adminGetShareHandler(w http.ResponseWriter, r *http.Request) {
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	dto := a.shareDTO(s, false)
	var daily []map[string]any
	rows, err := a.db.Query(`SELECT day, views, downloads, bytes FROM stats_daily WHERE share_id = ? ORDER BY day DESC LIMIT 30`, s.ID)
	if err == nil {
		for rows.Next() {
			var day string
			var views, downloads, bytes int64
			if rows.Scan(&day, &views, &downloads, &bytes) == nil {
				daily = append(daily, map[string]any{"day": day, "views": views, "downloads": downloads, "bytes": bytes})
			}
		}
		rows.Close()
	}
	dto["daily"] = daily
	writeJSON(w, http.StatusOK, map[string]any{"share": dto})
}

func (a *App) adminPatchShareHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	var req struct {
		Title           *string `json:"title"`
		Description     *string `json:"description"`
		AdminLocked     *bool   `json:"adminLocked"`
		AdminLockReason *string `json:"adminLockReason"`
		ExpiresAt       *int64  `json:"expiresAt"`
		DownloadLimit   *int64  `json:"downloadLimit"`
		Status          *string `json:"status"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	sets := []string{"updated_at = ?"}
	args := []any{util.Now()}
	revoke := false
	if req.Title != nil {
		if s.IsE2EE {
			writeError(w, http.StatusBadRequest, "e2ee_metadata_encrypted", "")
			return
		}
		if len(*req.Title) > 200 {
			writeError(w, http.StatusBadRequest, "metadata_too_long", "")
			return
		}
		sets = append(sets, "title = ?")
		args = append(args, nullString(strings.TrimSpace(*req.Title)))
	}
	if req.Description != nil {
		if s.IsE2EE {
			writeError(w, http.StatusBadRequest, "e2ee_metadata_encrypted", "")
			return
		}
		if len(*req.Description) > 4000 {
			writeError(w, http.StatusBadRequest, "metadata_too_long", "")
			return
		}
		sets = append(sets, "description = ?")
		args = append(args, nullString(*req.Description))
	}
	if req.AdminLocked != nil {
		sets = append(sets, "admin_locked = ?")
		args = append(args, boolInt(*req.AdminLocked))
		if *req.AdminLocked {
			reason := ""
			if req.AdminLockReason != nil {
				reason = *req.AdminLockReason
			}
			sets = append(sets, "admin_lock_reason = ?")
			args = append(args, nullString(reason))
			revoke = true
		} else {
			sets = append(sets, "admin_lock_reason = NULL")
		}
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt > 0 && *req.ExpiresAt <= util.Now() {
			writeError(w, http.StatusBadRequest, "invalid_expiry", "")
			return
		}
		sets = append(sets, "expires_at = ?")
		args = append(args, nullInt(*req.ExpiresAt))
	}
	if req.DownloadLimit != nil {
		if *req.DownloadLimit < 0 {
			writeError(w, http.StatusBadRequest, "invalid_download_limit", "")
			return
		}
		sets = append(sets, "download_limit = ?")
		args = append(args, *req.DownloadLimit)
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "paused":
			sets = append(sets, "status = ?")
			args = append(args, *req.Status)
		default:
			writeError(w, http.StatusBadRequest, "invalid_status", "")
			return
		}
	}
	args = append(args, s.ID)
	if _, err := a.db.Exec(`UPDATE shares SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if revoke {
		a.revokeShareAccess(s.ID)
	}
	updated, _ := a.shareByID(s.ID)
	a.audit(actor.ID, actor.Username, "share.admin_update", "share", s.ID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareDTO(updated, false)})
}

func (a *App) adminTrashShareHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	now := util.Now()
	purge := int64(0)
	if days := a.settings.Int("trash_retention_days"); days > 0 {
		purge = now + days*86400
	}
	if _, err := a.db.Exec(`UPDATE shares SET status = 'deleted', deleted_at = ?, purge_after = ?, updated_at = ? WHERE id = ?`, now, nullInt(purge), now, s.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.revokeShareAccess(s.ID)
	a.audit(actor.ID, actor.Username, "share.admin_trash", "share", s.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminRestoreShareHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if s.Status != "deleted" && s.Status != "expired" {
		writeError(w, http.StatusConflict, "not_in_trash", "")
		return
	}
	var req struct {
		ExpiresAt *int64 `json:"expiresAt"`
	}
	if err := readJSON(r, &req); err != nil || req.ExpiresAt == nil {
		writeError(w, http.StatusBadRequest, "expiry_required", "")
		return
	}
	if *req.ExpiresAt > 0 && *req.ExpiresAt <= util.Now() {
		writeError(w, http.StatusBadRequest, "invalid_expiry", "")
		return
	}
	ownerID := s.OwnerID
	ownerName := s.OwnerUsername
	if ownerID == 0 {
		ownerID = actor.ID
		ownerName = actor.Username
		if actor.StorageLimitBytes > 0 && actor.StorageUsedBytes+actor.StorageReservedBytes+s.SizeBytes > actor.StorageLimitBytes {
			writeError(w, http.StatusConflict, "storage_limit", "")
			return
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE shares SET status = 'active', deleted_at = NULL, purge_after = NULL, expires_at = ?, owner_id = ?, owner_username = ?, updated_at = ? WHERE id = ?`,
		nullInt(*req.ExpiresAt), ownerID, ownerName, util.Now(), s.ID); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if s.OwnerID == 0 {
		if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE id = ?`, s.SizeBytes, ownerID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.audit(actor.ID, actor.Username, "share.admin_restore", "share", s.ID, "success", nil)
	updated, _ := a.shareByID(s.ID)
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareSummary(updated)})
}

func (a *App) adminPurgeShareHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if err := a.purgeShare(s); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.audit(actor.ID, actor.Username, "share.admin_purge", "share", s.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminRotateTokenHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	token := util.RandomToken(32)
	if _, err := a.db.Exec(`UPDATE shares SET token_hash = ?, updated_at = ? WHERE id = ?`, util.HashToken(token), util.Now(), s.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.revokeShareAccess(s.ID)
	a.audit(actor.ID, actor.Username, "share.admin_token_rotated", "share", s.ID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "urlPath": a.shareURLPath(s) + "/" + token})
}

func (a *App) adminShareContentHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	dto := a.shareDTO(s, false)
	dto["encryptedOnly"] = s.IsE2EE
	if s.IsE2EE {
		dto["notice"] = "e2ee_admin_cannot_decrypt"
	}
	a.audit(actor.ID, actor.Username, "share.admin_view_content", "share", s.ID, "success", map[string]any{"owner": s.OwnerUsername, "e2ee": s.IsE2EE})
	writeJSON(w, http.StatusOK, map[string]any{"share": dto})
}

func (a *App) adminDownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	f, err := a.shareFile(s.ID, r.PathValue("fileId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	if f.ScanStatus == "infected" {
		writeError(w, http.StatusLocked, "file_quarantined", "")
		return
	}
	file, err := a.store.Open(f.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file_not_found", "")
		return
	}
	defer file.Close()
	a.audit(actor.ID, actor.Username, "share.admin_download", "share", s.ID, "success", map[string]any{"fileId": f.ID, "e2ee": s.IsE2EE})
	w.Header().Set("Content-Type", f.Mime)
	if s.IsE2EE {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	fileName := f.Name
	if s.IsE2EE || fileName == "" {
		fileName = f.ID + ".bin"
	}
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", fileName))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, fileName, time.Unix(f.CreatedAt, 0), file)
}

func (a *App) adminGlobalStatsHandler(w http.ResponseWriter, r *http.Request) {
	days := int(util.ParseInt(r.URL.Query().Get("days"), 30))
	if days < 1 || days > 365 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).UTC().Format("2006-01-02")
	rows, err := a.db.Query(`SELECT day, SUM(views), SUM(downloads), SUM(bytes) FROM stats_daily WHERE day >= ? GROUP BY day ORDER BY day ASC`, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	daily := []map[string]any{}
	for rows.Next() {
		var day string
		var views, downloads, bytes int64
		if rows.Scan(&day, &views, &downloads, &bytes) == nil {
			daily = append(daily, map[string]any{"day": day, "views": views, "downloads": downloads, "bytes": bytes})
		}
	}
	topRows, _ := a.db.Query(`SELECT s.id, s.slug, COALESCE(s.title,''), s.owner_username, s.view_count, s.download_count, s.bytes_served
		FROM shares s WHERE s.status NOT IN ('deleted','expired') ORDER BY s.download_count DESC, s.view_count DESC LIMIT 10`)
	top := []map[string]any{}
	if topRows != nil {
		for topRows.Next() {
			var id, slug, title, owner string
			var views, downloads, bytes int64
			if topRows.Scan(&id, &slug, &title, &owner, &views, &downloads, &bytes) == nil {
				top = append(top, map[string]any{"id": id, "slug": slug, "title": title, "owner": owner,
					"views": views, "downloads": downloads, "bytes": bytes})
			}
		}
		topRows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"daily": daily, "topShares": top})
}
