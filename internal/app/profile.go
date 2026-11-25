package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

var notificationTypes = map[string]bool{
	"upload_complete": true,
	"scan_complete":   true,
	"share_expiring":  true,
	"share_expired":   true,
	"share_purged":    true,
	"storage_warning": true,
	"security":        true,
}

func (a *App) profileHandler(w http.ResponseWriter, r *http.Request) {
	_, u := sessionFromContext(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": a.userDTO(u),
		"limits": map[string]any{
			"storageLimitBytes": u.StorageLimitBytes,
			"storageUsedBytes":  u.StorageUsedBytes,
			"storageReservedBytes": u.StorageReservedBytes,
		},
	})
}

func (a *App) updateProfileHandler(w http.ResponseWriter, r *http.Request) {
	_, u := sessionFromContext(r)
	var req struct {
		DisplayName       *string          `json:"displayName"`
		Language          *string          `json:"language"`
		Theme             *string          `json:"theme"`
		NotificationPrefs *map[string]bool `json:"notificationPrefs"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	sets := []string{"updated_at = ?"}
	args := []any{util.Now()}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if len(name) < 1 || len(name) > 80 {
			writeError(w, http.StatusBadRequest, "invalid_display_name", "")
			return
		}
		sets = append(sets, "display_name = ?")
		args = append(args, name)
	}
	if req.Language != nil {
		lang := *req.Language
		if lang != "auto" && lang != "de" && lang != "en" {
			writeError(w, http.StatusBadRequest, "invalid_language", "")
			return
		}
		sets = append(sets, "language = ?")
		args = append(args, lang)
	}
	if req.Theme != nil {
		theme := *req.Theme
		if theme != "system" && theme != "light" && theme != "dark" {
			writeError(w, http.StatusBadRequest, "invalid_theme", "")
			return
		}
		sets = append(sets, "theme = ?")
		args = append(args, theme)
	}
	if req.NotificationPrefs != nil {
		clean := map[string]bool{}
		for k, v := range *req.NotificationPrefs {
			if !notificationTypes[k] {
				writeError(w, http.StatusBadRequest, "invalid_notification_pref", "")
				return
			}
			clean[k] = v
		}
		sets = append(sets, "notification_prefs = ?")
		args = append(args, util.JSON(clean))
	}
	args = append(args, u.ID)
	if _, err := a.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	updated, err := a.userByID(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.audit(u.ID, u.Username, "profile.update", "user", fmt.Sprint(u.ID), "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"user": a.userDTO(updated)})
}

func (a *App) uploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
	_, u := sessionFromContext(r)
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
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
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
	default:
		writeError(w, http.StatusBadRequest, "unsupported_image", "")
		return
	}
	dir := filepath.Join(a.cfg.DataDir, "branding", "avatars")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	name := fmt.Sprintf("%d-%s%s", u.ID, util.RandomHex(4), ext)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	rel := "avatars/" + name
	if _, err := a.db.Exec(`UPDATE users SET avatar_path = ?, updated_at = ? WHERE id = ?`, rel, util.Now(), u.ID); err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if u.AvatarPath != "" {
		_ = os.Remove(filepath.Join(a.cfg.DataDir, "branding", filepath.Base(filepath.Dir(u.AvatarPath)), filepath.Base(u.AvatarPath)))
	}
	updated, _ := a.userByID(u.ID)
	a.audit(u.ID, u.Username, "profile.avatar", "user", fmt.Sprint(u.ID), "success", map[string]any{"bytes": len(data)})
	writeJSON(w, http.StatusOK, map[string]any{"user": a.userDTO(updated)})
}

func (a *App) deleteAvatarHandler(w http.ResponseWriter, r *http.Request) {
	_, u := sessionFromContext(r)
	if u.AvatarPath != "" {
		_ = os.Remove(filepath.Join(a.cfg.DataDir, "branding", filepath.Base(filepath.Dir(u.AvatarPath)), filepath.Base(u.AvatarPath)))
	}
	_, _ = a.db.Exec(`UPDATE users SET avatar_path = NULL, updated_at = ? WHERE id = ?`, util.Now(), u.ID)
	a.audit(u.ID, u.Username, "profile.avatar_delete", "user", fmt.Sprint(u.ID), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) avatarHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	if id <= 0 {
		http.NotFound(w, r)
		return
	}
	var rel string
	if err := a.db.QueryRow(`SELECT COALESCE(avatar_path,'') FROM users WHERE id = ?`, id).Scan(&rel); err != nil || rel == "" {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(rel, "avatars/") || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.cfg.DataDir, "branding", filepath.FromSlash(rel)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if fi != nil {
		http.ServeContent(w, r, filepath.Base(rel), fi.ModTime(), bytes.NewReader(data))
		return
	}
	_, _ = w.Write(data)
}

func (a *App) notificationsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	limit := int(util.ParseInt(r.URL.Query().Get("limit"), 50))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	where := "user_id = ?"
	args := []any{u.ID}
	if boolFromQuery(r, "unread") {
		where += " AND read_at IS NULL"
	}
	rows, err := a.db.Query(`SELECT id, type, data, COALESCE(read_at,0), created_at FROM notifications WHERE `+where+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, readAt, createdAt int64
		var typ, data string
		if rows.Scan(&id, &typ, &data, &readAt, &createdAt) != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "type": typ, "data": json.RawMessage(data), "readAt": readAt, "createdAt": createdAt})
	}
	var unread int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, u.ID).Scan(&unread)
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": unread})
}

func (a *App) readNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	now := util.Now()
	if req.All {
		_, _ = a.db.Exec(`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, now, u.ID)
	} else {
		if len(req.IDs) == 0 || len(req.IDs) > 200 {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(req.IDs)), ",")
		args := []any{now, u.ID}
		for _, id := range req.IDs {
			args = append(args, id)
		}
		_, _ = a.db.Exec(`UPDATE notifications SET read_at = ? WHERE user_id = ? AND id IN (`+placeholders+`)`, args...)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) deleteNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	_, _ = a.db.Exec(`DELETE FROM notifications WHERE user_id = ? AND read_at IS NOT NULL`, u.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) activityHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	limit := int(util.ParseInt(r.URL.Query().Get("limit"), 50))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := a.db.Query(`SELECT id, COALESCE(share_id,''), type, meta, created_at FROM activity
		WHERE user_id = ? ORDER BY id DESC LIMIT ?`, u.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, createdAt int64
		var shareID, typ, meta string
		if rows.Scan(&id, &shareID, &typ, &meta, &createdAt) != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "shareId": shareID, "type": typ, "meta": json.RawMessage(meta), "createdAt": createdAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": out})
}

func (a *App) publicDomainsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	domains, err := a.allowedDomainsFor(u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	def, _ := a.defaultDomain()
	defaultID := int64(0)
	if def != nil {
		defaultID = def.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains, "defaultDomainId": defaultID})
}

func (a *App) userStatsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r)
	now := util.Now()
	var shares, active, trashed, files int64
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE owner_id = ?`, u.ID).Scan(&shares)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE owner_id = ? AND status = 'active'`, u.ID).Scan(&active)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE owner_id = ? AND status = 'deleted'`, u.ID).Scan(&trashed)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM share_files f JOIN shares s ON s.id = f.share_id WHERE s.owner_id = ?`, u.ID).Scan(&files)
	var views, downloads, bytes int64
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(st.views),0), COALESCE(SUM(st.downloads),0), COALESCE(SUM(st.bytes),0)
		FROM stats_daily st JOIN shares s ON s.id = st.share_id WHERE s.owner_id = ?`, u.ID).Scan(&views, &downloads, &bytes)
	recent := []map[string]any{}
	rows, err := a.db.Query(`SELECT id, type, COALESCE(title,''), slug, size_bytes, download_count, view_count, created_at, status
		FROM shares WHERE owner_id = ? ORDER BY created_at DESC LIMIT 5`, u.ID)
	if err == nil {
		for rows.Next() {
			var id, typ, title, slug, status string
			var size, dl, vw, created int64
			if rows.Scan(&id, &typ, &title, &slug, &size, &dl, &vw, &created, &status) == nil {
				recent = append(recent, map[string]any{"id": id, "type": typ, "title": title, "slug": slug,
					"sizeBytes": size, "downloadCount": dl, "viewCount": vw, "createdAt": created, "status": status})
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shares": shares, "activeShares": active, "trashedShares": trashed, "files": files,
		"views": views, "downloads": downloads, "bytesServed": bytes,
		"storage": map[string]any{"used": u.StorageUsedBytes, "reserved": u.StorageReservedBytes, "limit": u.StorageLimitBytes},
		"recentShares": recent, "now": now,
	})
}
