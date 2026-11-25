package app

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) setupStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup":  !a.setupComplete(),
		"appName":     a.settings.Get("app_name"),
		"version":     a.cfg.Version,
		"maintenance": a.settings.Bool("maintenance_mode"),
	})
}

func (a *App) setupHandler(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "")
		return
	}
	if a.setupComplete() {
		writeError(w, http.StatusForbidden, "setup_completed", "")
		return
	}
	ip := util.ClientIP(r, a.cfg.TrustProxy)
	if !a.loginLimiter.Allow("setup:"+ip, 5, 5) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "")
		return
	}
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		AppName     string `json:"appName"`
		Domain      string `json:"domain"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
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
	minLen := int(a.settings.Int("password_min_length"))
	if err := auth.ValidatePasswordPolicy(req.Password, minLen); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if req.Domain == "" {
		req.Domain = "localhost"
	}
	if !util.ValidHostname(req.Domain) {
		writeError(w, http.StatusBadRequest, "invalid_domain", "")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	var admins int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if admins > 0 {
		tx.Rollback()
		writeError(w, http.StatusForbidden, "setup_completed", "")
		return
	}
	now := util.Now()
	res, err := tx.Exec(`INSERT INTO users(username, display_name, password_hash, role, must_change_password, created_at, updated_at)
		VALUES(?, ?, ?, 'admin', 0, ?, ?)`, req.Username, req.DisplayName, hash, now, now)
	if err != nil {
		tx.Rollback()
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "username_taken", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	adminID, _ := res.LastInsertId()
	var existingDomain int64
	err = tx.QueryRow(`SELECT id FROM domains WHERE hostname = ?`, req.Domain).Scan(&existingDomain)
	if err == sql.ErrNoRows {
		verified := 0
		if req.Domain == "localhost" {
			verified = 1
		}
		res, err = tx.Exec(`INSERT INTO domains(hostname, enabled, verified, verification_token, is_default, tls_status, created_at)
			VALUES(?, 1, ?, ?, 1, 'unknown', ?)`, req.Domain, verified, util.RandomHex(16), now)
		if err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		existingDomain, _ = res.LastInsertId()
	} else if err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO user_domains(user_id, domain_id) VALUES(?, ?)`, adminID, existingDomain); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES('default_domain_id', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, existingDomain); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES('setup_completed', '1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if req.AppName != "" && len(req.AppName) <= 60 {
		_ = a.settings.Set("app_name", req.AppName)
	}
	a.settings.SetMany(map[string]string{"setup_completed": "1", "default_domain_id": util.JSON(existingDomain)})
	a.refreshHostCache()
	u, err := a.userByID(adminID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	token, sess, err := a.createSession(adminID, "admin", false, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.setSessionCookie(w, r, "admin", token, sess.ExpiresAt, false)
	a.audit(adminID, u.Username, "setup.completed", "user", "1", "success", map[string]any{"domain": req.Domain})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": a.userDTO(u), "csrf": sess.CSRFToken})
}
