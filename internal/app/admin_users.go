package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) adminListUsersHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	role := r.URL.Query().Get("role")
	page, limit := parsePage(r)
	where := []string{"1=1"}
	args := []any{}
	if q != "" {
		where = append(where, "(username LIKE ? OR display_name LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if role == "admin" || role == "user" {
		where = append(where, "role = ?")
		args = append(args, role)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE `+whereSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	rows, err := a.db.Query(userSelect+` WHERE `+whereSQL+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			continue
		}
		out = append(out, a.adminUserDTO(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "total": total, "page": page, "limit": limit})
}

func (a *App) adminUserDTO(u *User) map[string]any {
	dto := a.userDTO(u)
	var shareCount int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE owner_id = ? AND status NOT IN ('deleted','expired')`, u.ID).Scan(&shareCount)
	dto["shareCount"] = shareCount
	dto["domains"] = a.userDomainIDs(u.ID)
	dto["storagePercent"] = storagePercent(u.StorageUsedBytes+u.StorageReservedBytes, u.StorageLimitBytes)
	return dto
}

func storagePercent(used, limit int64) float64 {
	if limit <= 0 {
		return 0
	}
	p := float64(used) / float64(limit) * 100
	if p > 100 {
		p = 100
	}
	return p
}

func (a *App) userDomainIDs(userID int64) []int64 {
	rows, err := a.db.Query(`SELECT domain_id FROM user_domains WHERE user_id = ? ORDER BY domain_id`, userID)
	if err != nil {
		return []int64{}
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) adminGetUserHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	var shares int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE owner_id = ?`, id).Scan(&shares)
	dto := a.adminUserDTO(u)
	dto["totalShares"] = shares
	writeJSON(w, http.StatusOK, map[string]any{"user": dto})
}

func (a *App) adminCreateUserHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	var req struct {
		Username         string  `json:"username"`
		DisplayName      string  `json:"displayName"`
		Password         string  `json:"password"`
		Role             string  `json:"role"`
		StorageLimitBytes *int64 `json:"storageLimitBytes"`
		Namespace        string  `json:"namespace"`
		DomainIDs        []int64 `json:"domainIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Namespace = strings.ToLower(strings.TrimSpace(req.Namespace))
	if !util.ValidUsername(req.Username) {
		writeError(w, http.StatusBadRequest, "invalid_username", "")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	if len(req.DisplayName) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_display_name", "")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role != "user" && req.Role != "admin" {
		writeError(w, http.StatusBadRequest, "invalid_role", "")
		return
	}
	if req.Namespace != "" && !util.ValidNamespace(req.Namespace) {
		writeError(w, http.StatusBadRequest, "invalid_namespace", "")
		return
	}
	limit := a.settings.Int("default_storage_limit_bytes")
	if req.StorageLimitBytes != nil {
		limit = *req.StorageLimitBytes
	}
	if limit < 0 {
		writeError(w, http.StatusBadRequest, "invalid_storage_limit", "")
		return
	}
	if limit == 0 && !a.settings.Bool("allow_unlimited_storage") {
		writeError(w, http.StatusBadRequest, "unlimited_storage_disabled", "")
		return
	}
	tempPassword := req.Password
	if tempPassword == "" {
		tempPassword = auth.GenerateTempPassword()
	} else {
		if err := auth.ValidatePasswordPolicy(tempPassword, int(a.settings.Int("password_min_length"))); err != nil {
			writeError(w, http.StatusBadRequest, "weak_password", err.Error())
			return
		}
	}
	hash, err := auth.HashPassword(tempPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	now := util.Now()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	res, err := tx.Exec(`INSERT INTO users(username, display_name, password_hash, role, must_change_password,
		namespace, storage_limit_bytes, created_at, updated_at) VALUES(?, ?, ?, ?, 1, ?, ?, ?, ?)`,
		req.Username, req.DisplayName, hash, req.Role, nullString(req.Namespace), limit, now, now)
	if err != nil {
		tx.Rollback()
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "username_taken", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	id, _ := res.LastInsertId()
	for _, did := range req.DomainIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_domains(user_id, domain_id) VALUES(?, ?)`, id, did); err != nil {
			tx.Rollback()
			writeError(w, http.StatusBadRequest, "invalid_domain", "")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	u, _ := a.userByID(id)
	a.notify(id, "security", map[string]any{"event": "account_created"})
	a.audit(actor.ID, actor.Username, "user.create", "user", fmt.Sprint(id), "success", map[string]any{"username": req.Username, "role": req.Role})
	writeJSON(w, http.StatusCreated, map[string]any{"user": a.adminUserDTO(u), "temporaryPassword": tempPassword})
}

func (a *App) adminPatchUserHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	var req struct {
		DisplayName       *string `json:"displayName"`
		Role              *string `json:"role"`
		StorageLimitBytes *int64  `json:"storageLimitBytes"`
		Namespace         *string `json:"namespace"`
		DomainIDs         *[]int64 `json:"domainIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	sets := []string{"updated_at = ?"}
	args := []any{util.Now()}
	if req.DisplayName != nil {
		if len(*req.DisplayName) == 0 || len(*req.DisplayName) > 80 {
			writeError(w, http.StatusBadRequest, "invalid_display_name", "")
			return
		}
		sets = append(sets, "display_name = ?")
		args = append(args, *req.DisplayName)
	}
	if req.Role != nil && *req.Role != u.Role {
		if *req.Role != "user" && *req.Role != "admin" {
			writeError(w, http.StatusBadRequest, "invalid_role", "")
			return
		}
		if u.Role == "admin" && *req.Role == "user" && a.countAdmins() <= 1 {
			writeError(w, http.StatusConflict, "last_admin", "")
			return
		}
		sets = append(sets, "role = ?")
		args = append(args, *req.Role)
	}
	if req.StorageLimitBytes != nil {
		if *req.StorageLimitBytes < 0 {
			writeError(w, http.StatusBadRequest, "invalid_storage_limit", "")
			return
		}
		if *req.StorageLimitBytes == 0 && !a.settings.Bool("allow_unlimited_storage") {
			writeError(w, http.StatusBadRequest, "unlimited_storage_disabled", "")
			return
		}
		sets = append(sets, "storage_limit_bytes = ?")
		args = append(args, *req.StorageLimitBytes)
	}
	if req.Namespace != nil {
		ns := strings.ToLower(strings.TrimSpace(*req.Namespace))
		if ns != "" && !util.ValidNamespace(ns) {
			writeError(w, http.StatusBadRequest, "invalid_namespace", "")
			return
		}
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE namespace = ? AND id <> ?`, ns, id).Scan(&n)
		if n > 0 {
			writeError(w, http.StatusConflict, "namespace_taken", "")
			return
		}
		sets = append(sets, "namespace = ?")
		args = append(args, nullString(ns))
	}
	args = append(args, id)
	if _, err := a.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if req.DomainIDs != nil {
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM user_domains WHERE user_id = ?`, id); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		for _, did := range *req.DomainIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO user_domains(user_id, domain_id) VALUES(?, ?)`, id, did); err != nil {
				tx.Rollback()
				writeError(w, http.StatusBadRequest, "invalid_domain", "")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	}
	updated, _ := a.userByID(id)
	a.audit(actor.ID, actor.Username, "user.update", "user", fmt.Sprint(id), "success", map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, map[string]any{"user": a.adminUserDTO(updated)})
}

func (a *App) adminSuspendUserHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	if u.ID == actor.ID {
		writeError(w, http.StatusConflict, "cannot_suspend_self", "")
		return
	}
	if u.Role == "admin" && a.countAdmins() <= 1 {
		writeError(w, http.StatusConflict, "last_admin", "")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = readJSON(r, &req)
	now := util.Now()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET is_suspended = 1, suspended_at = ?, suspended_reason = ?, updated_at = ? WHERE id = ?`, now, req.Reason, now, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE shares SET suspended_block = 1, updated_at = ? WHERE owner_id = ?`, now, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM share_access WHERE share_id IN (SELECT id FROM shares WHERE owner_id = ?)`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.notify(id, "security", map[string]any{"event": "account_suspended"})
	a.audit(actor.ID, actor.Username, "user.suspend", "user", fmt.Sprint(id), "success", map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) adminReactivateUserHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	now := util.Now()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET is_suspended = 0, suspended_at = NULL, suspended_reason = NULL, failed_login_count = 0, locked_until = NULL, updated_at = ? WHERE id = ?`, now, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE shares SET suspended_block = 0, updated_at = ? WHERE owner_id = ?`, now, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.notify(id, "security", map[string]any{"event": "account_reactivated"})
	a.audit(actor.ID, actor.Username, "user.reactivate", "user", fmt.Sprint(id), "success", map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) adminResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	_ = readJSON(r, &req)
	temp := req.Password
	if temp == "" {
		temp = auth.GenerateTempPassword()
	} else if err := auth.ValidatePasswordPolicy(temp, int(a.settings.Int("password_min_length"))); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	hash, err := auth.HashPassword(temp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash = ?, must_change_password = 1, updated_at = ? WHERE id = ?`, hash, util.Now(), id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.notify(id, "security", map[string]any{"event": "password_reset"})
	a.audit(actor.ID, actor.Username, "user.password_reset", "user", fmt.Sprint(id), "success", map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, map[string]any{"temporaryPassword": temp})
}

func (a *App) adminReset2FAHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET totp_secret = NULL, totp_enabled = 0, totp_last_step = 0 WHERE id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.notify(id, "security", map[string]any{"event": "2fa_reset_by_admin"})
	a.audit(actor.ID, actor.Username, "user.2fa_reset", "user", fmt.Sprint(id), "success", map[string]any{"username": u.Username})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) adminRevokeSessionsHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	if _, err := a.userByID(id); err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, id)
	a.audit(actor.ID, actor.Username, "user.sessions_revoked", "user", fmt.Sprint(id), "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) adminDeletePreviewHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	rows, err := a.db.Query(shareSelect+` WHERE s.owner_id = ? ORDER BY s.created_at DESC`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	shares := []map[string]any{}
	for rows.Next() {
		s, err := scanShare(rows)
		if err == nil {
			shares = append(shares, a.shareSummary(s))
		}
	}
	var otherUsers int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id <> ? AND is_suspended = 0`, id).Scan(&otherUsers)
	writeJSON(w, http.StatusOK, map[string]any{"user": a.adminUserDTO(u), "shares": shares, "otherUsers": otherUsers})
}

type deleteAssignment struct {
	ShareID      string `json:"shareId"`
	Action       string `json:"action"`
	TargetUserID int64  `json:"targetUserId"`
}

func (a *App) adminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	u, err := a.userByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user_not_found", "")
		return
	}
	if u.Role == "admin" && a.countAdmins() <= 1 {
		writeError(w, http.StatusConflict, "last_admin", "")
		return
	}
	var req struct {
		ConfirmUsername string             `json:"confirmUsername"`
		DefaultAction   string             `json:"defaultAction"`
		TransferTo      int64              `json:"transferTo"`
		Assignments     []deleteAssignment `json:"assignments"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if req.ConfirmUsername != u.Username {
		writeError(w, http.StatusBadRequest, "confirm_mismatch", "")
		return
	}
	shares := []*Share{}
	rows, err := a.db.Query(shareSelect+` WHERE s.owner_id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	for rows.Next() {
		if s, err := scanShare(rows); err == nil {
			shares = append(shares, s)
		}
	}
	rows.Close()
	assign := map[string]deleteAssignment{}
	for _, as := range req.Assignments {
		assign[as.ShareID] = as
	}
	now := util.Now()
	purgeAfter := int64(0)
	if days := a.settings.Int("trash_retention_days"); days > 0 {
		purgeAfter = now + days*86400
	}
	// Validate targets and aggregate transfer sizes.
	type transferTarget struct {
		user  *User
		bytes int64
	}
	targets := map[int64]*transferTarget{}
	resolveTarget := func(uid int64) (*transferTarget, error) {
		if uid == 0 {
			return nil, fmt.Errorf("target_required")
		}
		if t, ok := targets[uid]; ok {
			return t, nil
		}
		tu, err := a.userByID(uid)
		if err != nil {
			return nil, fmt.Errorf("target_not_found")
		}
		t := &transferTarget{user: tu}
		targets[uid] = t
		return t, nil
	}
	final := map[string]deleteAssignment{}
	for _, s := range shares {
		as, ok := assign[s.ID]
		if !ok {
			as = deleteAssignment{ShareID: s.ID, Action: req.DefaultAction, TargetUserID: req.TransferTo}
		}
		if s.Status == "deleted" || s.Status == "expired" {
			as.Action = "trash"
		}
		switch as.Action {
		case "trash":
		case "transfer":
			t, err := resolveTarget(as.TargetUserID)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error(), "")
				return
			}
			if t.user.ID == id {
				writeError(w, http.StatusBadRequest, "target_self", "")
				return
			}
			if t.user.Role != "admin" && !a.userDomainAllowed(t.user, s.DomainID) {
				writeError(w, http.StatusBadRequest, "target_domain_forbidden", "")
				return
			}
			t.bytes += s.SizeBytes
		default:
			writeError(w, http.StatusBadRequest, "assignments_required", "")
			return
		}
		final[s.ID] = as
	}
	for _, t := range targets {
		if t.user.StorageLimitBytes > 0 && t.user.StorageUsedBytes+t.user.StorageReservedBytes+t.bytes > t.user.StorageLimitBytes {
			writeError(w, http.StatusConflict, "target_storage_limit", "")
			return
		}
	}
	// Remove unfinished upload staging directories before the account row (and
	// with it the upload rows) disappears.
	if uploadRows, err := a.db.Query(`SELECT id FROM uploads WHERE user_id = ? AND status <> 'attached'`, id); err == nil {
		ids := []string{}
		for uploadRows.Next() {
			var uid string
			if uploadRows.Scan(&uid) == nil {
				ids = append(ids, uid)
			}
		}
		uploadRows.Close()
		for _, uid := range ids {
			_ = a.store.Remove(a.store.UploadDir(uid))
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	for _, s := range shares {
		as := final[s.ID]
		if as.Action == "transfer" {
			t := targets[as.TargetUserID]
			if _, err := tx.Exec(`UPDATE shares SET owner_id = ?, owner_username = ?, updated_at = ? WHERE id = ?`, t.user.ID, t.user.Username, now, s.ID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			if _, err := tx.Exec(`UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE id = ?`, s.SizeBytes, t.user.ID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
		} else {
			if _, err := tx.Exec(`UPDATE shares SET owner_id = NULL, status = 'deleted', deleted_at = COALESCE(deleted_at, ?), purge_after = ?, suspended_block = 0, updated_at = ? WHERE id = ?`,
				now, nullInt(purgeAfter), now, s.ID); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	for _, t := range targets {
		a.notify(t.user.ID, "security", map[string]any{"event": "shares_received", "from": u.Username})
	}
	a.audit(actor.ID, actor.Username, "user.delete", "user", fmt.Sprint(id), "success", map[string]any{
		"username": u.Username, "shares": len(shares), "defaultAction": req.DefaultAction,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
