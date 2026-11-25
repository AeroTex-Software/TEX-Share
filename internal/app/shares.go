package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/filetype"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

const shareSelect = `SELECT s.id, COALESCE(s.owner_id,0), s.owner_username, s.type, s.domain_id, d.hostname,
	COALESCE(d.enabled,0), COALESCE(d.verified,0), s.namespace, s.slug, s.token_hash,
	COALESCE(s.title,''), COALESCE(s.description,''), COALESCE(s.link_url,''), COALESCE(s.text_content,''),
	COALESCE(s.password_hash,''), s.is_e2ee, COALESCE(s.e2ee_mode,''), s.e2ee_manifest,
	COALESCE(s.e2ee_key_wrap,''), COALESCE(s.e2ee_recovery_wrap,''), s.status, s.admin_locked,
	COALESCE(s.admin_lock_reason,''), s.suspended_block, COALESCE(s.expires_at,0), s.download_limit,
	s.download_count, s.view_count, s.bytes_served, s.size_bytes, s.created_at, s.updated_at,
	COALESCE(s.deleted_at,0), COALESCE(s.purge_after,0), COALESCE(s.last_accessed_at,0), s.version,
	CASE WHEN s.password_hash IS NOT NULL AND s.password_hash <> '' THEN 1 ELSE 0 END,
	EXISTS(SELECT 1 FROM share_files f WHERE f.share_id = s.id AND f.scan_status IN ('pending','infected')),
	COALESCE(u.is_suspended,0)
	FROM shares s JOIN domains d ON d.id = s.domain_id LEFT JOIN users u ON u.id = s.owner_id`

func scanShare(row interface{ Scan(...any) error }) (*Share, error) {
	var s Share
	var e2ee, adminLocked, suspendedBlock, passwordSet, quarantine, ownerSuspended int
	var manifest []byte
	err := row.Scan(&s.ID, &s.OwnerID, &s.OwnerUsername, &s.Type, &s.DomainID, &s.DomainHostname,
		&s.DomainEnabled, &s.DomainVerified, &s.Namespace, &s.Slug, &s.TokenHash,
		&s.Title, &s.Description, &s.LinkURL, &s.TextContent, &s.PasswordHash, &e2ee, &s.E2EEMode,
		&manifest, &s.E2EEKeyWrap, &s.E2EERecoveryWrap, &s.Status, &adminLocked, &s.AdminLockReason,
		&suspendedBlock, &s.ExpiresAt, &s.DownloadLimit, &s.DownloadCount, &s.ViewCount, &s.BytesServed,
		&s.SizeBytes, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt, &s.PurgeAfter, &s.LastAccessedAt, &s.Version,
		&passwordSet, &quarantine, &ownerSuspended)
	if err != nil {
		return nil, err
	}
	s.IsE2EE = e2ee == 1
	s.AdminLocked = adminLocked == 1
	s.SuspendedBlock = suspendedBlock == 1
	s.PasswordSet = passwordSet == 1
	s.Quarantine = quarantine == 1
	s.OwnerSuspended = ownerSuspended == 1
	s.E2EEManifest = manifest
	s.EffectiveStatus = effectiveStatus(&s)
	return &s, nil
}

func effectiveStatus(s *Share) string {
	if s.Status == "deleted" {
		return "deleted"
	}
	if s.AdminLocked {
		return "locked"
	}
	if s.SuspendedBlock || s.OwnerSuspended || s.OwnerID == 0 {
		return "suspended"
	}
	if s.ExpiresAt > 0 && s.ExpiresAt <= util.Now() {
		return "expired"
	}
	if s.Status == "expired" {
		return "expired"
	}
	if s.Quarantine {
		return "quarantined"
	}
	if s.Status == "paused" {
		return "paused"
	}
	if !s.DomainEnabled {
		return "disabled"
	}
	return "active"
}

func (a *App) shareByID(id string) (*Share, error) {
	row := a.db.QueryRow(shareSelect+` WHERE s.id = ?`, id)
	return scanShare(row)
}

func (a *App) shareByToken(token string) (*Share, error) {
	row := a.db.QueryRow(shareSelect+` WHERE s.token_hash = ?`, util.HashToken(token))
	return scanShare(row)
}

func (a *App) shareFiles(shareID string) ([]ShareFile, error) {
	rows, err := a.db.Query(`SELECT id, share_id, position, name, mime, size_bytes, encrypted_size_bytes,
		storage_path, sha256, scan_status, COALESCE(scan_message,''), created_at
		FROM share_files WHERE share_id = ? ORDER BY position ASC, created_at ASC`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShareFile{}
	for rows.Next() {
		var f ShareFile
		if err := rows.Scan(&f.ID, &f.ShareID, &f.Position, &f.Name, &f.Mime, &f.SizeBytes, &f.EncryptedSizeBytes,
			&f.StoragePath, &f.SHA256, &f.ScanStatus, &f.ScanMessage, &f.CreatedAt); err != nil {
			continue
		}
		f.PreviewKind = filetype.PreviewKind(f.Name, f.Mime)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (a *App) shareFile(shareID, fileID string) (*ShareFile, error) {
	var f ShareFile
	err := a.db.QueryRow(`SELECT id, share_id, position, name, mime, size_bytes, encrypted_size_bytes,
		storage_path, sha256, scan_status, COALESCE(scan_message,''), created_at
		FROM share_files WHERE share_id = ? AND id = ?`, shareID, fileID).
		Scan(&f.ID, &f.ShareID, &f.Position, &f.Name, &f.Mime, &f.SizeBytes, &f.EncryptedSizeBytes,
			&f.StoragePath, &f.SHA256, &f.ScanStatus, &f.ScanMessage, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.PreviewKind = filetype.PreviewKind(f.Name, f.Mime)
	return &f, nil
}

func (a *App) shareDTO(s *Share, includeSecret bool) map[string]any {
	files, _ := a.shareFiles(s.ID)
	dto := map[string]any{
		"id": s.ID, "ownerId": s.OwnerID, "ownerUsername": s.OwnerUsername, "type": s.Type,
		"domainId": s.DomainID, "domainHostname": s.DomainHostname, "namespace": s.Namespace, "slug": s.Slug,
		"title": s.Title, "description": s.Description, "linkUrl": s.LinkURL, "textContent": s.TextContent,
		"isE2ee": s.IsE2EE, "e2eeMode": s.E2EEMode, "status": s.Status, "effectiveStatus": s.EffectiveStatus,
		"adminLocked": s.AdminLocked, "adminLockReason": s.AdminLockReason, "suspendedBlock": s.SuspendedBlock,
		"expiresAt": s.ExpiresAt, "downloadLimit": s.DownloadLimit, "downloadCount": s.DownloadCount,
		"viewCount": s.ViewCount, "bytesServed": s.BytesServed, "sizeBytes": s.SizeBytes,
		"createdAt": s.CreatedAt, "updatedAt": s.UpdatedAt, "deletedAt": s.DeletedAt, "purgeAfter": s.PurgeAfter,
		"lastAccessedAt": s.LastAccessedAt, "passwordSet": s.PasswordSet, "quarantine": s.Quarantine,
		"files": files, "urlPath": a.shareURLPath(s), "domainVerified": s.DomainVerified,
		"ownerSuspended": s.OwnerSuspended,
	}
	if s.IsE2EE {
		dto["e2eeManifest"] = base64.StdEncoding.EncodeToString(s.E2EEManifest)
		dto["e2eeKeyWrap"] = s.E2EEKeyWrap
		dto["e2eeRecoveryWrap"] = s.E2EERecoveryWrap
	}
	if includeSecret {
		// Tokens are only returned at creation and rotation, never on reads.
		dto["token"] = ""
	}
	return dto
}

type createShareRequest struct {
	Type        string   `json:"type"`
	DomainID    int64    `json:"domainId"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	LinkURL     string   `json:"linkUrl"`
	TextContent string   `json:"textContent"`
	UploadIDs   []string `json:"uploadIds"`
	Password    string   `json:"password"`
	ExpiresAt   int64    `json:"expiresAt"`
	DownloadLimit int64  `json:"downloadLimit"`
	E2EE        *struct {
		Mode         string `json:"mode"`
		Manifest     string `json:"manifest"`
		KeyWrap      string `json:"keyWrap"`
		RecoveryWrap string `json:"recoveryWrap"`
	} `json:"e2ee"`
}

func (a *App) createShareHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	if a.settings.Bool("maintenance_mode") {
		writeError(w, http.StatusServiceUnavailable, "maintenance", "")
		return
	}
	var req createShareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	share, token, files, err := a.createShare(u, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	a.activity(u.ID, share.ID, "share_created", map[string]any{"type": share.Type})
	a.notify(u.ID, "upload_complete", map[string]any{"shareId": share.ID, "slug": share.Slug, "type": share.Type})
	a.audit(u.ID, u.Username, "share.create", "share", share.ID, "success", map[string]any{"type": share.Type, "files": len(files)})
	dto := a.shareDTO(share, true)
	dto["token"] = token
	dto["urlPath"] = a.shareURLPath(share) + "/" + token
	dto["sharePath"] = a.shareURLPath(share)
	writeJSON(w, http.StatusCreated, map[string]any{"share": dto})
}

func (a *App) createShare(u *User, req *createShareRequest) (*Share, string, []ShareFile, error) {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	switch req.Type {
	case "file", "link", "text", "media", "folder":
	default:
		return nil, "", nil, fmt.Errorf("invalid_share_type")
	}
	domain, err := a.domainByID(req.DomainID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("invalid_domain")
	}
	if !domain.Enabled {
		return nil, "", nil, fmt.Errorf("domain_disabled")
	}
	if !a.userDomainAllowed(u, domain.ID) {
		return nil, "", nil, fmt.Errorf("domain_forbidden")
	}
	if a.settings.Bool("require_domain_verification") && !domain.Verified {
		return nil, "", nil, fmt.Errorf("domain_unverified")
	}
	if req.ExpiresAt > 0 && req.ExpiresAt <= util.Now() {
		return nil, "", nil, fmt.Errorf("invalid_expiry")
	}
	if req.DownloadLimit < 0 {
		return nil, "", nil, fmt.Errorf("invalid_download_limit")
	}
	if req.Password != "" && len(req.Password) < 8 {
		return nil, "", nil, fmt.Errorf("share_password_too_short")
	}
	if req.Password != "" && len(req.Password) > 256 {
		return nil, "", nil, fmt.Errorf("invalid_password")
	}
	isE2EE := req.E2EE != nil
	if isE2EE {
		if req.E2EE.Mode != "key" && req.E2EE.Mode != "password" {
			return nil, "", nil, fmt.Errorf("invalid_e2ee_mode")
		}
		if req.E2EE.Manifest == "" {
			return nil, "", nil, fmt.Errorf("e2ee_manifest_required")
		}
		if req.E2EE.RecoveryWrap == "" {
			return nil, "", nil, fmt.Errorf("e2ee_recovery_required")
		}
		if req.E2EE.Mode == "password" && req.E2EE.KeyWrap == "" {
			return nil, "", nil, fmt.Errorf("e2ee_key_wrap_required")
		}
		if req.E2EE.Mode == "key" && req.E2EE.KeyWrap != "" {
			return nil, "", nil, fmt.Errorf("invalid_e2ee_key_wrap")
		}
		if _, err := base64.StdEncoding.DecodeString(req.E2EE.Manifest); err != nil {
			return nil, "", nil, fmt.Errorf("invalid_e2ee_manifest")
		}
		if len(req.E2EE.Manifest) > 4<<20 {
			return nil, "", nil, fmt.Errorf("e2ee_manifest_too_large")
		}
		// Encrypted metadata must never be stored in plaintext fields.
		req.Title, req.Description, req.LinkURL, req.TextContent = "", "", "", ""
	} else {
		if len(req.Title) > 200 || len(req.Description) > 4000 {
			return nil, "", nil, fmt.Errorf("metadata_too_long")
		}
	}
	if req.Type == "link" {
		if isE2EE {
			if len(req.UploadIDs) > 0 {
				return nil, "", nil, fmt.Errorf("unexpected_files")
			}
		} else {
			u, err := url.Parse(req.LinkURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return nil, "", nil, fmt.Errorf("invalid_link_url")
			}
			if len(req.LinkURL) > 4096 {
				return nil, "", nil, fmt.Errorf("invalid_link_url")
			}
		}
	}
	if req.Type == "text" && !isE2EE && len(req.TextContent) > 2<<20 {
		return nil, "", nil, fmt.Errorf("text_too_large")
	}
	if req.Type == "text" && !isE2EE && strings.TrimSpace(req.TextContent) == "" {
		return nil, "", nil, fmt.Errorf("text_required")
	}
	if (req.Type == "file" || req.Type == "media" || req.Type == "folder") && len(req.UploadIDs) == 0 {
		return nil, "", nil, fmt.Errorf("files_required")
	}
	if req.Type == "file" && len(req.UploadIDs) != 1 {
		return nil, "", nil, fmt.Errorf("exactly_one_file_required")
	}
	if len(req.UploadIDs) > 500 {
		return nil, "", nil, fmt.Errorf("too_many_files")
	}
	if req.Type == "link" || req.Type == "text" {
		if len(req.UploadIDs) > 0 {
			return nil, "", nil, fmt.Errorf("unexpected_files")
		}
	}
	maxShareSize := a.settings.Int("max_share_size_bytes")
	var uploads []*Upload
	var totalSize int64
	for _, id := range req.UploadIDs {
		up, err := a.uploadByID(id)
		if err != nil || up.UserID != u.ID {
			return nil, "", nil, fmt.Errorf("upload_not_found")
		}
		if up.Status != "complete" {
			return nil, "", nil, fmt.Errorf("upload_incomplete")
		}
		if up.Encrypted != isE2EE {
			return nil, "", nil, fmt.Errorf("upload_encryption_mismatch")
		}
		if up.ExpiresAt <= util.Now() {
			return nil, "", nil, fmt.Errorf("upload_expired")
		}
		totalSize += up.SizeBytes
		uploads = append(uploads, up)
	}
	if maxShareSize > 0 && totalSize > maxShareSize {
		return nil, "", nil, fmt.Errorf("share_size_limit")
	}
	slug := util.Slugify(req.Slug)
	if req.Slug != "" {
		if !util.ValidSlug(slug) || len(slug) > 64 {
			return nil, "", nil, fmt.Errorf("invalid_slug")
		}
	}
	if slug == "" {
		slug = util.Slugify(req.Title)
	}
	if slug == "" {
		slug = "share-" + util.RandomHex(4)
	}
	namespace := u.Namespace
	var exists int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE domain_id = ? AND namespace = ? AND slug = ?`, domain.ID, namespace, slug).Scan(&exists)
	if exists > 0 {
		if req.Slug != "" {
			return nil, "", nil, fmt.Errorf("slug_taken")
		}
		for i := 0; i < 5; i++ {
			candidate := slug + "-" + util.RandomHex(2)
			if len(candidate) > 64 {
				candidate = candidate[:64]
			}
			_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE domain_id = ? AND namespace = ? AND slug = ?`, domain.ID, namespace, candidate).Scan(&exists)
			if exists == 0 {
				slug = candidate
				break
			}
		}
		if exists > 0 {
			return nil, "", nil, fmt.Errorf("slug_taken")
		}
	}
	var passwordHash string
	if req.Password != "" {
		passwordHash, err = auth.HashPassword(req.Password)
		if err != nil {
			return nil, "", nil, fmt.Errorf("internal_error")
		}
	}
	now := util.Now()
	shareID := util.RandomHex(16)
	token := util.RandomToken(32)
	tokenHash := util.HashToken(token)
	var manifest []byte
	keyWrap, recoveryWrap, mode := "", "", ""
	if isE2EE {
		manifest, _ = base64.StdEncoding.DecodeString(req.E2EE.Manifest)
		keyWrap, recoveryWrap, mode = req.E2EE.KeyWrap, req.E2EE.RecoveryWrap, req.E2EE.Mode
	}
	tx, err := a.db.Begin()
	if err != nil {
		return nil, "", nil, fmt.Errorf("internal_error")
	}
	_, err = tx.Exec(`INSERT INTO shares(id, owner_id, owner_username, type, domain_id, namespace, slug, token_hash,
		title, description, link_url, text_content, password_hash, is_e2ee, e2ee_mode, e2ee_manifest,
		e2ee_key_wrap, e2ee_recovery_wrap, status, expires_at, download_limit, size_bytes, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?)`,
		shareID, u.ID, u.Username, req.Type, domain.ID, namespace, slug, tokenHash,
		nullString(req.Title), nullString(req.Description), nullString(req.LinkURL), nullString(req.TextContent),
		nullString(passwordHash), boolInt(isE2EE), nullString(mode), manifest, nullString(keyWrap), nullString(recoveryWrap),
		nullInt(req.ExpiresAt), req.DownloadLimit, totalSize, now, now)
	if err != nil {
		tx.Rollback()
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, "", nil, fmt.Errorf("slug_taken")
		}
		return nil, "", nil, fmt.Errorf("internal_error")
	}
	files := make([]ShareFile, 0, len(uploads))
	moved := make([]struct{ from, to string }, 0, len(uploads))
	for i, up := range uploads {
		destRel := fmt.Sprintf("files/%s/%s.bin", shareID, up.ID)
		fileName, fileMime := cleanShareName(up.Filename), up.Mime
		if isE2EE {
			fileName, fileMime = "", "application/octet-stream"
		}
		_, err := tx.Exec(`INSERT INTO share_files(id, share_id, position, name, mime, size_bytes, encrypted_size_bytes,
			storage_path, sha256, scan_status, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			up.ID, shareID, i, fileName, fileMime, up.SizeBytes, boolInt64(up.Encrypted, up.SizeBytes),
			destRel, up.SHA256, up.ScanStatus, now)
		if err != nil {
			tx.Rollback()
			return nil, "", nil, fmt.Errorf("internal_error")
		}
		if _, err := tx.Exec(`UPDATE uploads SET status = 'attached', storage_path = ?, updated_at = ? WHERE id = ?`, destRel, now, up.ID); err != nil {
			tx.Rollback()
			return nil, "", nil, fmt.Errorf("internal_error")
		}
		if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = storage_used_bytes + ?, storage_reserved_bytes = CASE WHEN storage_reserved_bytes >= ? THEN storage_reserved_bytes - ? ELSE 0 END WHERE id = ?`,
			up.SizeBytes, up.SizeBytes, up.SizeBytes, u.ID); err != nil {
			tx.Rollback()
			return nil, "", nil, fmt.Errorf("internal_error")
		}
		finalRel, size, err := a.store.Attach(up.ID, shareID, up.ID)
		if err != nil {
			tx.Rollback()
			for _, m := range moved {
				_ = a.store.Move(m.to, m.from)
			}
			return nil, "", nil, fmt.Errorf("storage_error")
		}
		moved = append(moved, struct{ from, to string }{from: fmt.Sprintf("uploads/%s/blob.part", up.ID), to: finalRel})
		f := ShareFile{ID: up.ID, ShareID: shareID, Position: i, Name: fileName, Mime: fileMime,
			SizeBytes: up.SizeBytes, EncryptedSizeBytes: boolInt64(up.Encrypted, up.SizeBytes), StoragePath: finalRel,
			SHA256: up.SHA256, ScanStatus: up.ScanStatus, CreatedAt: now}
		f.PreviewKind = filetype.PreviewKind(f.Name, f.Mime)
		files = append(files, f)
		_ = size
	}
	if err := tx.Commit(); err != nil {
		for _, m := range moved {
			_ = a.store.Move(m.to, m.from)
		}
		return nil, "", nil, fmt.Errorf("internal_error")
	}
	share := &Share{ID: shareID, OwnerID: u.ID, OwnerUsername: u.Username, Type: req.Type, DomainID: domain.ID,
		DomainHostname: domain.Hostname, DomainEnabled: domain.Enabled, DomainVerified: domain.Verified,
		Namespace: namespace, Slug: slug, TokenHash: tokenHash, Title: req.Title, Description: req.Description,
		LinkURL: req.LinkURL, TextContent: req.TextContent, PasswordHash: passwordHash, IsE2EE: isE2EE,
		E2EEMode: mode, E2EEManifest: manifest, E2EEKeyWrap: keyWrap, E2EERecoveryWrap: recoveryWrap,
		Status: "active", ExpiresAt: req.ExpiresAt, DownloadLimit: req.DownloadLimit, SizeBytes: totalSize,
		CreatedAt: now, UpdatedAt: now, Files: files, PasswordSet: passwordHash != "", Version: 1}
	share.EffectiveStatus = effectiveStatus(share)
	return share, token, files, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func boolInt64(cond bool, v int64) int64 {
	if cond {
		return v
	}
	return 0
}

func cleanShareName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		out = append(out, util.SanitizeFilename(p))
	}
	if len(out) == 0 {
		return "file"
	}
	joined := strings.Join(out, "/")
	if len(joined) > 512 {
		joined = joined[len(joined)-512:]
	}
	return joined
}

func (a *App) listSharesHandler(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		status := r.URL.Query().Get("status")
		typ := r.URL.Query().Get("type")
		trash := boolFromQuery(r, "trash")
		page, limit := parsePage(r)
		where := []string{}
		args := []any{}
		if !admin {
			where = append(where, "s.owner_id = ?")
			args = append(args, u.ID)
		}
		if trash {
			where = append(where, "(s.status = 'deleted' OR s.status = 'expired')")
		} else {
			where = append(where, "s.status NOT IN ('deleted','expired')")
		}
		if status != "" && status != "all" {
			where = append(where, "s.status = ?")
			args = append(args, status)
		}
		if typ != "" && typ != "all" {
			where = append(where, "s.type = ?")
			args = append(args, typ)
		}
		if q != "" {
			where = append(where, "(s.slug LIKE ? OR COALESCE(s.title,'') LIKE ? OR s.owner_username LIKE ?)")
			like := "%" + q + "%"
			args = append(args, like, like, like)
		}
		whereSQL := " WHERE " + strings.Join(where, " AND ")
		var total int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM shares s`+whereSQL, args...).Scan(&total); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		rows, err := a.db.Query(shareSelect+whereSQL+` ORDER BY s.updated_at DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			s, err := scanShare(rows)
			if err != nil {
				continue
			}
			out = append(out, a.shareSummary(s))
		}
		writeJSON(w, http.StatusOK, map[string]any{"shares": out, "total": total, "page": page, "limit": limit})
	}
}

func (a *App) shareSummary(s *Share) map[string]any {
	return map[string]any{
		"id": s.ID, "type": s.Type, "title": s.Title, "description": s.Description, "slug": s.Slug,
		"namespace": s.Namespace, "domainId": s.DomainID, "domainHostname": s.DomainHostname,
		"isE2ee": s.IsE2EE, "status": s.Status, "effectiveStatus": s.EffectiveStatus, "adminLocked": s.AdminLocked,
		"quarantine": s.Quarantine, "sizeBytes": s.SizeBytes, "viewCount": s.ViewCount, "downloadCount": s.DownloadCount,
		"downloadLimit": s.DownloadLimit, "expiresAt": s.ExpiresAt, "createdAt": s.CreatedAt, "updatedAt": s.UpdatedAt,
		"deletedAt": s.DeletedAt, "purgeAfter": s.PurgeAfter, "passwordSet": s.PasswordSet, "ownerUsername": s.OwnerUsername,
		"ownerId": s.OwnerID, "urlPath": a.shareURLPath(s), "ownerSuspended": s.OwnerSuspended,
	}
}
