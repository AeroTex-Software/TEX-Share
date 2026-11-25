package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) getShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareDTO(s, false)})
}

type sharePatchRequest struct {
	Title         *string `json:"title"`
	Description   *string `json:"description"`
	Slug          *string `json:"slug"`
	DomainID      *int64  `json:"domainId"`
	LinkURL       *string `json:"linkUrl"`
	TextContent   *string `json:"textContent"`
	Password      *string `json:"password"`
	ExpiresAt     *int64  `json:"expiresAt"`
	DownloadLimit *int64  `json:"downloadLimit"`
	Status        *string `json:"status"`
	E2EEManifest  *string `json:"e2eeManifest"`
}

func (a *App) patchShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if s.Status == "deleted" || s.Status == "expired" {
		writeError(w, http.StatusConflict, "share_in_trash", "")
		return
	}
	var req sharePatchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	sets := []string{"updated_at = ?"}
	args := []any{util.Now()}
	revokeAccess := false
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
	if req.LinkURL != nil {
		if s.IsE2EE || s.Type != "link" {
			writeError(w, http.StatusBadRequest, "invalid_field", "")
			return
		}
		parsed, err := url.Parse(*req.LinkURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || len(*req.LinkURL) > 4096 {
			writeError(w, http.StatusBadRequest, "invalid_link_url", "")
			return
		}
		sets = append(sets, "link_url = ?")
		args = append(args, *req.LinkURL)
	}
	if req.TextContent != nil {
		if s.IsE2EE || s.Type != "text" {
			writeError(w, http.StatusBadRequest, "invalid_field", "")
			return
		}
		if len(*req.TextContent) > 2<<20 {
			writeError(w, http.StatusBadRequest, "text_too_large", "")
			return
		}
		sets = append(sets, "text_content = ?")
		args = append(args, nullString(*req.TextContent))
	}
	if req.E2EEManifest != nil {
		if !s.IsE2EE {
			writeError(w, http.StatusBadRequest, "not_encrypted", "")
			return
		}
		raw, err := base64.StdEncoding.DecodeString(*req.E2EEManifest)
		if err != nil || len(raw) == 0 || len(raw) > 4<<20 {
			writeError(w, http.StatusBadRequest, "invalid_e2ee_manifest", "")
			return
		}
		sets = append(sets, "e2ee_manifest = ?")
		args = append(args, raw)
	}
	if req.Slug != nil {
		slug := util.Slugify(*req.Slug)
		if !util.ValidSlug(slug) || len(slug) > 64 {
			writeError(w, http.StatusBadRequest, "invalid_slug", "")
			return
		}
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE domain_id = ? AND namespace = ? AND slug = ? AND id <> ?`,
			s.DomainID, s.Namespace, slug, s.ID).Scan(&n)
		if n > 0 {
			writeError(w, http.StatusConflict, "slug_taken", "")
			return
		}
		sets = append(sets, "slug = ?")
		args = append(args, slug)
	}
	if req.DomainID != nil && *req.DomainID != s.DomainID {
		d, err := a.domainByID(*req.DomainID)
		if err != nil || !d.Enabled || !a.userDomainAllowed(u, d.ID) {
			writeError(w, http.StatusBadRequest, "invalid_domain", "")
			return
		}
		if a.settings.Bool("require_domain_verification") && !d.Verified {
			writeError(w, http.StatusBadRequest, "domain_unverified", "")
			return
		}
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE domain_id = ? AND namespace = ? AND slug = ? AND id <> ?`,
			d.ID, s.Namespace, s.Slug, s.ID).Scan(&n)
		if n > 0 {
			writeError(w, http.StatusConflict, "slug_taken", "")
			return
		}
		sets = append(sets, "domain_id = ?")
		args = append(args, d.ID)
	}
	if req.Password != nil {
		if *req.Password == "" {
			sets = append(sets, "password_hash = NULL")
		} else {
			if len(*req.Password) < 8 || len(*req.Password) > 256 {
				writeError(w, http.StatusBadRequest, "share_password_too_short", "")
				return
			}
			hash, err := auth.HashPassword(*req.Password)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			sets = append(sets, "password_hash = ?")
			args = append(args, hash)
		}
		revokeAccess = true
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
			if s.AdminLocked {
				writeError(w, http.StatusConflict, "admin_locked", "")
				return
			}
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
	if revokeAccess {
		a.revokeShareAccess(s.ID)
	}
	updated, _ := a.shareByID(s.ID)
	a.activity(u.ID, s.ID, "share_updated", nil)
	a.audit(u.ID, u.Username, "share.update", "share", s.ID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareDTO(updated, false)})
}

func (a *App) trashShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if s.Status == "deleted" || s.Status == "expired" {
		writeError(w, http.StatusConflict, "already_in_trash", "")
		return
	}
	days := a.settings.Int("trash_retention_days")
	if days < 0 {
		days = 30
	}
	now := util.Now()
	var purgeAfter any
	if days > 0 {
		purgeAfter = now + days*86400
	}
	_, err = a.db.Exec(`UPDATE shares SET status = 'deleted', deleted_at = ?, purge_after = ?, updated_at = ? WHERE id = ?`, now, purgeAfter, now, s.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.revokeShareAccess(s.ID)
	a.activity(u.ID, s.ID, "share_trashed", nil)
	a.audit(u.ID, u.Username, "share.trash", "share", s.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) restoreShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
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
	_, err = a.db.Exec(`UPDATE shares SET status = 'active', deleted_at = NULL, purge_after = NULL, expires_at = ?, updated_at = ? WHERE id = ?`,
		nullInt(*req.ExpiresAt), util.Now(), s.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.activity(u.ID, s.ID, "share_restored", nil)
	a.audit(u.ID, u.Username, "share.restore", "share", s.ID, "success", nil)
	updated, _ := a.shareByID(s.ID)
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareSummary(updated)})
}

func (a *App) purgeShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if s.Status != "deleted" && s.Status != "expired" {
		writeError(w, http.StatusConflict, "not_in_trash", "")
		return
	}
	if err := a.purgeShare(s); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.audit(u.ID, u.Username, "share.purge", "share", s.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) purgeShare(s *Share) error {
	files, _ := a.shareFiles(s.ID)
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	for _, f := range files {
		_, _ = tx.Exec(`DELETE FROM uploads WHERE id = ?`, f.ID)
	}
	if _, err := tx.Exec(`DELETE FROM shares WHERE id = ?`, s.ID); err != nil {
		tx.Rollback()
		return err
	}
	if s.OwnerID > 0 {
		if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = CASE WHEN storage_used_bytes >= ? THEN storage_used_bytes - ? ELSE 0 END WHERE id = ?`,
			s.SizeBytes, s.SizeBytes, s.OwnerID); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = a.store.RemoveShare(s.ID)
	return nil
}

func (a *App) rotateTokenHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	s, err := a.shareByID(r.PathValue("id"))
	if err != nil || s.OwnerID != u.ID {
		writeError(w, http.StatusNotFound, "share_not_found", "")
		return
	}
	if s.Status == "deleted" || s.Status == "expired" {
		writeError(w, http.StatusConflict, "share_in_trash", "")
		return
	}
	token := util.RandomToken(32)
	if _, err := a.db.Exec(`UPDATE shares SET token_hash = ?, updated_at = ? WHERE id = ?`, util.HashToken(token), util.Now(), s.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.revokeShareAccess(s.ID)
	a.audit(u.ID, u.Username, "share.token_rotated", "share", s.ID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "urlPath": a.shareURLPath(s) + "/" + token, "sharePath": a.shareURLPath(s),
	})
}

func (a *App) rotateKeyHandler(w http.ResponseWriter, r *http.Request) {
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
	if s.Status == "deleted" || s.Status == "expired" {
		writeError(w, http.StatusConflict, "share_in_trash", "")
		return
	}
	var req struct {
		Manifest     string   `json:"manifest"`
		KeyWrap      string   `json:"keyWrap"`
		RecoveryWrap string   `json:"recoveryWrap"`
		UploadIDs    []string `json:"uploadIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	manifest, err := base64.StdEncoding.DecodeString(req.Manifest)
	if err != nil || len(manifest) == 0 || len(manifest) > 4<<20 {
		writeError(w, http.StatusBadRequest, "invalid_e2ee_manifest", "")
		return
	}
	if req.RecoveryWrap == "" {
		writeError(w, http.StatusBadRequest, "e2ee_recovery_required", "")
		return
	}
	if s.E2EEMode == "password" && req.KeyWrap == "" {
		writeError(w, http.StatusBadRequest, "e2ee_key_wrap_required", "")
		return
	}
	if s.E2EEMode == "key" && req.KeyWrap != "" {
		writeError(w, http.StatusBadRequest, "invalid_e2ee_key_wrap", "")
		return
	}
	hasFiles := s.Type == "file" || s.Type == "media" || s.Type == "folder"
	if hasFiles {
		if len(req.UploadIDs) == 0 {
			writeError(w, http.StatusBadRequest, "files_required", "")
			return
		}
	} else if len(req.UploadIDs) > 0 {
		writeError(w, http.StatusBadRequest, "unexpected_files", "")
		return
	}
	uploads := make([]*Upload, 0, len(req.UploadIDs))
	var newSize int64
	for _, id := range req.UploadIDs {
		up, err := a.uploadByID(id)
		if err != nil || up.UserID != u.ID || up.Status != "complete" || !up.Encrypted {
			writeError(w, http.StatusBadRequest, "upload_not_found", "")
			return
		}
		newSize += up.SizeBytes
		uploads = append(uploads, up)
	}
	oldFiles, _ := a.shareFiles(s.ID)
	now := util.Now()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE shares SET e2ee_manifest = ?, e2ee_key_wrap = ?, e2ee_recovery_wrap = ?, size_bytes = ?, version = version + 1, updated_at = ? WHERE id = ?`,
		manifest, nullString(req.KeyWrap), req.RecoveryWrap, newSize, now, s.ID); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	moved := []struct{ from, to string }{}
	if hasFiles {
		if _, err := tx.Exec(`DELETE FROM share_files WHERE share_id = ?`, s.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		for i, up := range uploads {
			destRel := fmt.Sprintf("files/%s/%s.bin", s.ID, up.ID)
			if _, err := tx.Exec(`INSERT INTO share_files(id, share_id, position, name, mime, size_bytes, encrypted_size_bytes,
				storage_path, sha256, scan_status, created_at) VALUES(?, ?, ?, '', 'application/octet-stream', ?, ?, ?, ?, 'encrypted', ?)`,
				up.ID, s.ID, i, up.SizeBytes, up.SizeBytes, destRel, up.SHA256, now); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			if _, err := tx.Exec(`UPDATE uploads SET status = 'attached', storage_path = ?, updated_at = ? WHERE id = ?`, destRel, now, up.ID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = storage_used_bytes + ?, storage_reserved_bytes = CASE WHEN storage_reserved_bytes >= ? THEN storage_reserved_bytes - ? ELSE 0 END WHERE id = ?`,
				up.SizeBytes, up.SizeBytes, up.SizeBytes, u.ID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			finalRel, _, err := a.store.Attach(up.ID, s.ID, up.ID)
			if err != nil {
				tx.Rollback()
				for _, m := range moved {
					_ = a.store.Move(m.to, m.from)
				}
				writeError(w, http.StatusInternalServerError, "storage_error", "")
				return
			}
			moved = append(moved, struct{ from, to string }{from: fmt.Sprintf("uploads/%s/blob.part", up.ID), to: finalRel})
		}
		if s.OwnerID > 0 {
			if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = CASE WHEN storage_used_bytes >= ? THEN storage_used_bytes - ? ELSE 0 END WHERE id = ?`,
				s.SizeBytes, s.SizeBytes, s.OwnerID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		for _, m := range moved {
			_ = a.store.Move(m.to, m.from)
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if hasFiles {
		for _, f := range oldFiles {
			_ = a.store.Remove(f.StoragePath)
		}
	}
	a.revokeShareAccess(s.ID)
	a.activity(u.ID, s.ID, "share_key_rotated", nil)
	a.audit(u.ID, u.Username, "share.key_rotated", "share", s.ID, "success", nil)
	updated, _ := a.shareByID(s.ID)
	writeJSON(w, http.StatusOK, map[string]any{"share": a.shareDTO(updated, false)})
}
