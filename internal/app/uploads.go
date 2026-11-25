package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/AeroTex-Software/TEX-Share/internal/filetype"
	"github.com/AeroTex-Software/TEX-Share/internal/storage"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

const uploadChunkSize = 4 << 20

const uploadSelect = `SELECT id, user_id, filename, mime, size_bytes, chunk_size, total_chunks, encrypted, status,
	COALESCE(storage_path,''), sha256, scan_status, created_at, updated_at, expires_at FROM uploads`

func scanUpload(row interface{ Scan(...any) error }) (*Upload, error) {
	var u Upload
	var enc int
	err := row.Scan(&u.ID, &u.UserID, &u.Filename, &u.Mime, &u.SizeBytes, &u.ChunkSize, &u.TotalChunks, &enc,
		&u.Status, &u.StoragePath, &u.SHA256, &u.ScanStatus, &u.CreatedAt, &u.UpdatedAt, &u.ExpiresAt)
	if err != nil {
		return nil, err
	}
	u.Encrypted = enc == 1
	return &u, nil
}

func (a *App) uploadByID(id string) (*Upload, error) {
	return scanUpload(a.db.QueryRow(uploadSelect+` WHERE id = ?`, id))
}

func (a *App) reserveQuota(userID, size int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var limit, used, reserved int64
	if err := tx.QueryRow(`SELECT storage_limit_bytes, storage_used_bytes, storage_reserved_bytes FROM users WHERE id = ?`, userID).
		Scan(&limit, &used, &reserved); err != nil {
		return err
	}
	if limit > 0 && used+reserved+size > limit {
		return fmt.Errorf("storage_limit")
	}
	globalLimit := a.settings.Int("global_storage_limit_bytes")
	if globalLimit > 0 {
		var total sql.NullInt64
		if err := tx.QueryRow(`SELECT SUM(storage_used_bytes + storage_reserved_bytes) FROM users`).Scan(&total); err != nil {
			return err
		}
		if total.Int64+size > globalLimit {
			return fmt.Errorf("global_storage_limit")
		}
	}
	if _, err := tx.Exec(`UPDATE users SET storage_reserved_bytes = storage_reserved_bytes + ? WHERE id = ?`, size, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) refundQuota(userID, size int64) {
	if size <= 0 {
		return
	}
	_, _ = a.db.Exec(`UPDATE users SET storage_reserved_bytes = CASE WHEN storage_reserved_bytes >= ? THEN storage_reserved_bytes - ? ELSE 0 END WHERE id = ?`, size, size, userID)
}

func (a *App) uploadInitHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var req struct {
		Filename  string `json:"filename"`
		Size      int64  `json:"size"`
		Mime      string `json:"mime"`
		Encrypted bool   `json:"encrypted"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "empty_file", "")
		return
	}
	maxFile := a.settings.Int("max_file_size_bytes")
	if maxFile > 0 && req.Size > maxFile {
		writeError(w, http.StatusRequestEntityTooLarge, "file_size_limit", "")
		return
	}
	if req.Size > 1<<40 {
		writeError(w, http.StatusRequestEntityTooLarge, "file_size_limit", "")
		return
	}
	filename := cleanShareName(req.Filename)
	if req.Encrypted {
		filename = "encrypted"
		req.Mime = "application/octet-stream"
	} else {
		blocked, _ := filetype.CheckUpload(filename, req.Mime, nil, a.blockedExtensions(), a.blockedMimes())
		if blocked {
			writeError(w, http.StatusBadRequest, "file_type_blocked", "")
			return
		}
	}
	if req.Mime == "" {
		req.Mime = "application/octet-stream"
	}
	if len(req.Mime) > 200 {
		req.Mime = "application/octet-stream"
	}
	var active int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM uploads WHERE user_id = ? AND status IN ('pending','uploading')`, u.ID).Scan(&active)
	queueLimit := int(a.settings.Int("upload_queue_limit"))
	if queueLimit > 0 && active >= queueLimit {
		writeError(w, http.StatusTooManyRequests, "upload_queue_full", "")
		return
	}
	if err := a.reserveQuota(u.ID, req.Size); err != nil {
		if err.Error() == "global_storage_limit" {
			writeError(w, http.StatusInsufficientStorage, "global_storage_limit", "")
		} else {
			writeError(w, http.StatusRequestEntityTooLarge, "storage_limit", "")
		}
		return
	}
	uploadID := util.RandomHex(16)
	if err := a.store.EnsureUpload(uploadID); err != nil {
		a.refundQuota(u.ID, req.Size)
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	threshold := a.settings.Int("resumable_threshold_bytes")
	// Encrypted containers are produced on the fly and must stream to disk
	// without buffering the whole ciphertext, so always use resumable chunks.
	chunked := req.Encrypted || (req.Size >= threshold && threshold > 0)
	chunkSize := int64(0)
	totalChunks := 0
	mode := "direct"
	if chunked {
		chunkSize = uploadChunkSize
		totalChunks = int((req.Size + chunkSize - 1) / chunkSize)
		mode = "chunked"
	}
	now := util.Now()
	_, err := a.db.Exec(`INSERT INTO uploads(id, user_id, filename, mime, size_bytes, chunk_size, total_chunks, encrypted,
		status, staging_dir, created_at, updated_at, expires_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
		uploadID, u.ID, filename, req.Mime, req.Size, chunkSize, totalChunks, boolInt(req.Encrypted),
		a.store.UploadDir(uploadID), now, now, now+86400)
	if err != nil {
		a.refundQuota(u.ID, req.Size)
		_ = a.store.Remove(a.store.UploadDir(uploadID))
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": uploadID, "mode": mode, "chunkSize": chunkSize, "totalChunks": totalChunks,
		"size": req.Size, "filename": filename, "expiresAt": now + 86400,
	})
}

func (a *App) directUploadHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := r.PathValue("id")
	up, err := a.uploadByID(id)
	if err != nil || up.UserID != u.ID {
		writeError(w, http.StatusNotFound, "upload_not_found", "")
		return
	}
	if up.Status == "complete" {
		writeError(w, http.StatusConflict, "upload_already_complete", "")
		return
	}
	if up.Status == "attached" || up.Status == "cancelled" {
		writeError(w, http.StatusGone, "upload_not_available", "")
		return
	}
	release, ok := a.gates.TryAcquire(fmt.Sprintf("upload:%d", u.ID), int(a.settings.Int("upload_concurrency_per_user")), int(a.settings.Int("global_upload_limit")))
	if !ok {
		writeError(w, http.StatusTooManyRequests, "upload_limit", "")
		return
	}
	defer release()
	if r.ContentLength != up.SizeBytes {
		writeError(w, http.StatusBadRequest, "size_mismatch", "")
		return
	}
	_, _ = a.db.Exec(`UPDATE uploads SET status = 'uploading', updated_at = ? WHERE id = ?`, util.Now(), up.ID)
	relPath, header, digest, size, err := a.store.WriteDirect(up.ID, r.Body, up.SizeBytes)
	if err != nil {
		if err == storage.ErrTooLarge {
			a.cancelUpload(up, true)
			writeError(w, http.StatusRequestEntityTooLarge, "file_size_limit", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "upload_failed", "")
		return
	}
	if size != up.SizeBytes {
		a.cancelUpload(up, true)
		writeError(w, http.StatusBadRequest, "size_mismatch", "")
		return
	}
	if !up.Encrypted {
		if blocked, _ := filetype.CheckUpload(up.Filename, up.Mime, header, a.blockedExtensions(), a.blockedMimes()); blocked {
			a.cancelUpload(up, true)
			writeError(w, http.StatusBadRequest, "file_type_blocked", "")
			return
		}
	}
	a.finalizeUpload(up, relPath, digest)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": up.ID, "scanStatus": a.currentScanStatus(up)})
}

func (a *App) chunkUploadHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeError(w, http.StatusBadRequest, "invalid_chunk", "")
		return
	}
	up, err := a.uploadByID(id)
	if err != nil || up.UserID != u.ID {
		writeError(w, http.StatusNotFound, "upload_not_found", "")
		return
	}
	if up.Status == "complete" || up.Status == "attached" {
		writeError(w, http.StatusConflict, "upload_already_complete", "")
		return
	}
	if up.Status == "cancelled" {
		writeError(w, http.StatusGone, "upload_not_available", "")
		return
	}
	if up.TotalChunks <= 0 || index >= up.TotalChunks {
		writeError(w, http.StatusBadRequest, "invalid_chunk", "")
		return
	}
	expected := up.ChunkSize
	if index == up.TotalChunks-1 {
		expected = up.SizeBytes - int64(index)*up.ChunkSize
	}
	if r.ContentLength >= 0 && r.ContentLength != expected {
		writeError(w, http.StatusBadRequest, "size_mismatch", "")
		return
	}
	release, ok := a.gates.TryAcquire(fmt.Sprintf("upload:%d", u.ID), int(a.settings.Int("upload_concurrency_per_user")), int(a.settings.Int("global_upload_limit")))
	if !ok {
		writeError(w, http.StatusTooManyRequests, "upload_limit", "")
		return
	}
	defer release()
	lock := a.uploadLock(up.ID)
	lock.Lock()
	_, _ = a.db.Exec(`UPDATE uploads SET status = 'uploading', updated_at = ? WHERE id = ?`, util.Now(), up.ID)
	lock.Unlock()
	n, err := a.store.WriteChunk(up.ID, index, r.Body, expected)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "")
		return
	}
	if n != expected {
		writeError(w, http.StatusBadRequest, "size_mismatch", "")
		return
	}
	info, _ := a.store.ReceivedChunks(up.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "received": len(info.Indices), "total": up.TotalChunks, "indices": info.Indices})
}

func (a *App) uploadStatusHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	up, err := a.uploadByID(r.PathValue("id"))
	if err != nil || up.UserID != u.ID {
		writeError(w, http.StatusNotFound, "upload_not_found", "")
		return
	}
	info, _ := a.store.ReceivedChunks(up.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"upload": up, "received": info.Indices, "receivedBytes": info.Bytes, "totalChunks": up.TotalChunks,
	})
}

func (a *App) completeUploadHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	up, err := a.uploadByID(r.PathValue("id"))
	if err != nil || up.UserID != u.ID {
		writeError(w, http.StatusNotFound, "upload_not_found", "")
		return
	}
	if up.Status == "complete" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "scanStatus": up.ScanStatus})
		return
	}
	if up.Status != "uploading" && up.Status != "pending" {
		writeError(w, http.StatusGone, "upload_not_available", "")
		return
	}
	lock := a.uploadLock(up.ID)
	lock.Lock()
	defer lock.Unlock()
	if up.TotalChunks <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_chunk", "")
		return
	}
	relPath, header, digest, err := a.store.AssembleChunks(up.ID, up.TotalChunks, up.SizeBytes)
	if err != nil {
		if err == storage.ErrIncomplete {
			writeError(w, http.StatusConflict, "upload_incomplete", "")
		} else {
			writeError(w, http.StatusInternalServerError, "upload_failed", "")
		}
		return
	}
	if !up.Encrypted {
		if blocked, _ := filetype.CheckUpload(up.Filename, up.Mime, header, a.blockedExtensions(), a.blockedMimes()); blocked {
			a.cancelUpload(up, true)
			writeError(w, http.StatusBadRequest, "file_type_blocked", "")
			return
		}
	}
	a.finalizeUpload(up, relPath, digest)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "scanStatus": a.currentScanStatus(up)})
}

func (a *App) cancelUploadHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	up, err := a.uploadByID(r.PathValue("id"))
	if err != nil || up.UserID != u.ID {
		writeError(w, http.StatusNotFound, "upload_not_found", "")
		return
	}
	if up.Status == "attached" {
		writeError(w, http.StatusConflict, "upload_attached", "")
		return
	}
	a.cancelUpload(up, true)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) uploadLock(id string) *sync.Mutex {
	v, _ := a.uploadLocks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (a *App) finalizeUpload(up *Upload, relPath, digest string) {
	scanStatus := "skipped"
	if up.Encrypted {
		scanStatus = "encrypted"
	} else if a.clamavEnabled() {
		scanStatus = "pending"
	}
	_, _ = a.db.Exec(`UPDATE uploads SET status = 'complete', storage_path = ?, sha256 = ?, scan_status = ?, updated_at = ? WHERE id = ?`,
		relPath, digest, scanStatus, util.Now(), up.ID)
	up.Status = "complete"
	up.StoragePath = relPath
	up.SHA256 = digest
	up.ScanStatus = scanStatus
	a.activity(up.UserID, "", "upload_complete", map[string]any{"filename": up.Filename, "size": up.SizeBytes})
	a.notify(up.UserID, "upload_complete", map[string]any{"filename": up.Filename, "uploadId": up.ID})
	if scanStatus == "pending" {
		a.queueScan(up.ID)
	}
}

func (a *App) currentScanStatus(up *Upload) string {
	var s string
	_ = a.db.QueryRow(`SELECT scan_status FROM uploads WHERE id = ?`, up.ID).Scan(&s)
	if s == "" {
		return up.ScanStatus
	}
	return s
}

func (a *App) cancelUpload(up *Upload, refund bool) {
	lock := a.uploadLock(up.ID)
	lock.Lock()
	defer lock.Unlock()
	var current string
	if err := a.db.QueryRow(`SELECT status FROM uploads WHERE id = ?`, up.ID).Scan(&current); err != nil {
		return
	}
	if current == "attached" || current == "cancelled" {
		return
	}
	_, _ = a.db.Exec(`UPDATE uploads SET status = 'cancelled', updated_at = ? WHERE id = ?`, util.Now(), up.ID)
	_ = a.store.Remove(a.store.UploadDir(up.ID))
	if refund {
		a.refundQuota(up.UserID, up.SizeBytes)
	}
}

// cleanupExpiredUploads is invoked periodically.
func (a *App) cleanupExpiredUploads() {
	rows, err := a.db.Query(`SELECT id, user_id, filename, mime, size_bytes, chunk_size, total_chunks, encrypted, status,
		COALESCE(storage_path,''), sha256, scan_status, created_at, updated_at, expires_at FROM uploads
		WHERE status IN ('pending','uploading','complete') AND expires_at < ?`, util.Now())
	if err != nil {
		return
	}
	list := []*Upload{}
	for rows.Next() {
		if up, err := scanUpload(rows); err == nil {
			list = append(list, up)
		}
	}
	rows.Close()
	for _, up := range list {
		a.cancelUpload(up, true)
	}
}

func (a *App) blockedExtensions() []string {
	return jsonStringList(a.settings.Get("blocked_extensions"))
}

func (a *App) blockedMimes() []string {
	return jsonStringList(a.settings.Get("blocked_mime"))
}

func jsonStringList(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
