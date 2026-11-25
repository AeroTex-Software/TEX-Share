package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/auth"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

const (
	publicCookie = "texshare_pub"
	adminCookie  = "texshare_adm"
	shareCookiePrefix = "texshare_share_"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
)

var (
	errNoSession    = errors.New("no session")
	errSuspended    = errors.New("account suspended")
	errPasswordChange = errors.New("password change required")
)

func cookieName(realm string) string {
	if realm == "admin" {
		return adminCookie
	}
	return publicCookie
}

func (a *App) cookieSecure(r *http.Request) bool {
	if a.cfg.ForceSecure {
		return true
	}
	if a.isHTTPS(r) {
		return true
	}
	if strings.HasPrefix(a.cfg.PublicURL, "https://") || strings.HasPrefix(a.cfg.AdminURL, "https://") {
		return true
	}
	return false
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request, realm, token string, expires int64, remember bool) {
	c := &http.Cookie{
		Name:     cookieName(realm),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	}
	if remember {
		c.Expires = time.Unix(expires, 0)
		c.MaxAge = int(time.Until(time.Unix(expires, 0)).Seconds())
	}
	http.SetCookie(w, c)
}

func (a *App) clearSessionCookie(w http.ResponseWriter, r *http.Request, realm string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(realm),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) createSession(userID int64, realm string, remember bool, ua string) (string, *Session, error) {
	token := util.RandomToken(32)
	csrf := util.RandomToken(24)
	now := util.Now()
	hours := a.settings.Int("max_session_hours")
	if hours < 1 {
		hours = 72
	}
	expires := now + hours*3600
	if remember {
		days := a.settings.Int("remember_me_days")
		if days < 1 {
			days = 30
		}
		expires = now + days*86400
	}
	browser, osName := auth.ParseUA(ua)
	res, err := a.db.Exec(`INSERT INTO sessions(token_hash, user_id, realm, csrf_token, remember, user_agent, browser, os, created_at, last_seen_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		util.HashToken(token), userID, realm, csrf, boolInt(remember), truncate(ua, 512), browser, osName, now, now, expires)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return token, &Session{ID: id, UserID: userID, Realm: realm, CSRFToken: csrf, Remember: remember,
		UserAgent: truncate(ua, 512), Browser: browser, OS: osName, CreatedAt: now, LastSeenAt: now, ExpiresAt: expires, Current: true}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (a *App) loadSession(r *http.Request, realm string) (*Session, *User, error) {
	c, err := r.Cookie(cookieName(realm))
	if err != nil || c.Value == "" {
		return nil, nil, errNoSession
	}
	var s Session
	var userID int64
	err = a.db.QueryRow(`SELECT id, user_id, realm, csrf_token, remember, user_agent, browser, os, created_at, last_seen_at, expires_at
		FROM sessions WHERE token_hash = ?`, util.HashToken(c.Value)).
		Scan(&s.ID, &userID, &s.Realm, &s.CSRFToken, &s.Remember, &s.UserAgent, &s.Browser, &s.OS, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt)
	if err != nil {
		return nil, nil, errNoSession
	}
	s.UserID = userID
	if s.Realm != realm || s.ExpiresAt <= util.Now() {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE id = ?`, s.ID)
		return nil, nil, errNoSession
	}
	u, err := a.userByID(userID)
	if err != nil {
		return nil, nil, errNoSession
	}
	if u.Suspended {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
		return nil, nil, errSuspended
	}
	if realm == "admin" && u.Role != "admin" {
		return nil, nil, errNoSession
	}
	if util.Now()-s.LastSeenAt > 300 {
		_, _ = a.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, util.Now(), s.ID)
		s.LastSeenAt = util.Now()
	}
	return &s, u, nil
}

// requireUser authenticates an API request against the given realm and enforces
// CSRF on state-changing methods.
func (a *App) requireUser(realm string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u, err := a.loadSession(r, realm)
		if err != nil {
			code := "unauthorized"
			status := http.StatusUnauthorized
			if errors.Is(err, errSuspended) {
				code = "account_suspended"
				status = http.StatusForbidden
			}
			writeError(w, status, code, "")
			return
		}
		if realm == "admin" && u.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		if isMutating(r.Method) {
			if !a.csrfOK(r, s) {
				writeError(w, http.StatusForbidden, "csrf_failed", "")
				return
			}
		}
		if u.MustChangePassword && !allowedDuringPasswordChange(r.URL.Path) {
			writeError(w, http.StatusForbidden, "password_change_required", "")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxSession, s)
		next(w, r.WithContext(ctx))
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func allowedDuringPasswordChange(path string) bool {
	return strings.HasSuffix(path, "/auth/change-password") ||
		strings.HasSuffix(path, "/auth/logout") ||
		strings.HasSuffix(path, "/auth/me")
}

func (a *App) csrfOK(r *http.Request, s *Session) bool {
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		return false
	}
	return util.ConstantEqual(token, s.CSRFToken)
}

func userFromContext(r *http.Request) *User {
	u, _ := r.Context().Value(ctxUser).(*User)
	return u
}

func sessionFromContext(r *http.Request) (*Session, *User) {
	s, _ := r.Context().Value(ctxSession).(*Session)
	return s, userFromContext(r)
}

// sameOrigin rejects cross-site state changes for unauthenticated endpoints.
func (a *App) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		ref := r.Header.Get("Referer")
		if ref == "" {
			return true
		}
		origin = ref
	}
	if origin == "null" {
		return false
	}
	origin = strings.TrimRight(origin, "/")
	u := origin
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	host := u
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if strings.EqualFold(host, r.Host) {
		return true
	}
	for _, base := range []string{a.cfg.PublicURL, a.cfg.AdminURL} {
		if base != "" {
			b := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
			if strings.EqualFold(host, b) {
				return true
			}
		}
	}
	return false
}

func (a *App) userByID(id int64) (*User, error) {
	row := a.db.QueryRow(userSelect+` WHERE id = ?`, id)
	return scanUser(row)
}

func (a *App) userByUsername(username string) (*User, error) {
	row := a.db.QueryRow(userSelect+` WHERE username = ?`, username)
	return scanUser(row)
}

const userSelect = `SELECT id, username, display_name, password_hash, role, must_change_password, is_suspended,
	COALESCE(suspended_at,0), COALESCE(suspended_reason,''), COALESCE(namespace,''), storage_limit_bytes,
	storage_used_bytes, storage_reserved_bytes, COALESCE(avatar_path,''), totp_enabled, totp_last_step,
	language, theme, notification_prefs, created_at, updated_at, COALESCE(last_login_at,0),
	failed_login_count, COALESCE(locked_until,0) FROM users`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var mustChange, suspended, totpEnabled int
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &mustChange, &suspended,
		&u.SuspendedAt, &u.SuspendedReason, &u.Namespace, &u.StorageLimitBytes, &u.StorageUsedBytes,
		&u.StorageReservedBytes, &u.AvatarPath, &totpEnabled, &u.TOTPLastStep, &u.Language, &u.Theme,
		&u.NotificationPrefs, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.FailedLoginCount, &u.LockedUntil)
	if err != nil {
		return nil, err
	}
	u.MustChangePassword = mustChange == 1
	u.Suspended = suspended == 1
	u.TOTPEnabled = totpEnabled == 1
	return &u, nil
}

func (a *App) loginHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(r) {
			writeError(w, http.StatusForbidden, "origin_rejected", "")
			return
		}
		ip := a.clientIP(r)
		rate := float64(a.settings.Int("login_rate_per_minute"))
		if rate <= 0 {
			rate = 10
		}
		if !a.loginLimiter.Allow("login:"+ip, rate, rate) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Remember bool   `json:"remember"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || len(req.Password) > 256 || len(req.Username) > 128 {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "")
			return
		}
		u, err := a.userByUsername(req.Username)
		if err != nil {
			// Equalise timing for unknown users.
			_ = auth.VerifyPassword(req.Password, a.dummyHash)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "")
			return
		}
		if u.LockedUntil > util.Now() {
			writeError(w, http.StatusTooManyRequests, "account_locked", "")
			return
		}
		if !auth.VerifyPassword(req.Password, u.PasswordHash) {
			failures := u.FailedLoginCount + 1
			limit := int(a.settings.Int("login_failures_before_lock"))
			if limit <= 0 {
				limit = 5
			}
			var lockedUntil any
			if failures >= limit {
				minutes := a.settings.Int("lockout_minutes")
				if minutes <= 0 {
					minutes = 15
				}
				lockedUntil = util.Now() + minutes*60
			}
			_, _ = a.db.Exec(`UPDATE users SET failed_login_count = ?, locked_until = ? WHERE id = ?`, failures, lockedUntil, u.ID)
			a.audit(u.ID, u.Username, "auth.login_failed", "user", fmt.Sprint(u.ID), "failure", map[string]any{"realm": realm})
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "")
			return
		}
		if u.Suspended {
			a.audit(u.ID, u.Username, "auth.login_suspended", "user", fmt.Sprint(u.ID), "failure", map[string]any{"realm": realm})
			writeError(w, http.StatusForbidden, "account_suspended", "Dein Benutzerkonto wurde durch einen Administrator suspendiert.")
			return
		}
		if realm == "admin" && u.Role != "admin" {
			a.audit(u.ID, u.Username, "auth.admin_login_denied", "user", fmt.Sprint(u.ID), "failure", nil)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "")
			return
		}
		_, _ = a.db.Exec(`UPDATE users SET failed_login_count = 0, locked_until = NULL WHERE id = ?`, u.ID)
		if u.TOTPEnabled {
			challenge := util.RandomToken(32)
			_, err := a.db.Exec(`INSERT INTO login_challenges(token_hash, user_id, realm, remember, created_at, expires_at)
				VALUES(?, ?, ?, ?, ?, ?)`, util.HashToken(challenge), u.ID, realm, boolInt(req.Remember), util.Now(), util.Now()+300)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
			writeJSON(w, http.StatusOK, LoginResult{TOTPRequired: true, Challenge: challenge})
			return
		}
		token, sess, err := a.createSession(u.ID, realm, req.Remember, r.UserAgent())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		_, _ = a.db.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, util.Now(), u.ID)
		a.setSessionCookie(w, r, realm, token, sess.ExpiresAt, req.Remember)
		a.afterLogin(u, realm, r)
		writeJSON(w, http.StatusOK, LoginResult{OK: true, User: a.userDTO(u), CSRF: sess.CSRFToken})
	}
}

func (a *App) afterLogin(u *User, realm string, r *http.Request) {
	a.activity(u.ID, "", "login", map[string]any{"realm": realm, "browser": authBrowser(r)})
	a.notify(u.ID, "security", map[string]any{"event": "new_login", "realm": realm, "browser": authBrowser(r)})
	a.audit(u.ID, u.Username, "auth.login", "user", fmt.Sprint(u.ID), "success", map[string]any{"realm": realm, "browser": authBrowser(r)})
}

func authBrowser(r *http.Request) string {
	b, o := auth.ParseUA(r.UserAgent())
	if o == "" {
		return b
	}
	return b + " / " + o
}

func (a *App) verifyTOTPHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(r) {
			writeError(w, http.StatusForbidden, "origin_rejected", "")
			return
		}
		ip := a.clientIP(r)
		if !a.loginLimiter.Allow("2fa:"+ip, 10, 10) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		var req struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
			Recovery  bool   `json:"recovery"`
		}
		if err := readJSON(r, &req); err != nil || req.Challenge == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		var ch struct {
			UserID   int64
			Realm    string
			Remember bool
			Attempts int
		}
		err := a.db.QueryRow(`SELECT user_id, realm, remember, attempts FROM login_challenges WHERE token_hash = ? AND expires_at > ?`,
			util.HashToken(req.Challenge), util.Now()).Scan(&ch.UserID, &ch.Realm, &ch.Remember, &ch.Attempts)
		if err != nil || ch.Realm != realm {
			writeError(w, http.StatusUnauthorized, "challenge_expired", "")
			return
		}
		maxAttempts := int(a.settings.Int("twofa_attempts"))
		if maxAttempts <= 0 {
			maxAttempts = 5
		}
		if ch.Attempts >= maxAttempts {
			_, _ = a.db.Exec(`DELETE FROM login_challenges WHERE token_hash = ?`, util.HashToken(req.Challenge))
			writeError(w, http.StatusTooManyRequests, "too_many_attempts", "")
			return
		}
		u, err := a.userByID(ch.UserID)
		if err != nil || u.Suspended {
			writeError(w, http.StatusUnauthorized, "challenge_expired", "")
			return
		}
		if realm == "admin" && u.Role != "admin" {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "")
			return
		}
		ok := false
		if req.Recovery {
			ok = a.consumeRecoveryCode(u.ID, strings.TrimSpace(req.Code))
		} else {
			secret, err := a.totpSecret(u.ID)
			if err == nil {
				var step int64
				step, ok = auth.ValidateTOTP(secret, req.Code, u.TOTPLastStep)
				if ok {
					_, _ = a.db.Exec(`UPDATE users SET totp_last_step = ? WHERE id = ?`, step, u.ID)
				}
			}
		}
		if !ok {
			_, _ = a.db.Exec(`UPDATE login_challenges SET attempts = attempts + 1 WHERE token_hash = ?`, util.HashToken(req.Challenge))
			writeError(w, http.StatusUnauthorized, "invalid_code", "")
			return
		}
		_, _ = a.db.Exec(`DELETE FROM login_challenges WHERE token_hash = ?`, util.HashToken(req.Challenge))
		token, sess, err := a.createSession(u.ID, realm, ch.Remember, r.UserAgent())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		_, _ = a.db.Exec(`UPDATE users SET last_login_at = ?, failed_login_count = 0, locked_until = NULL WHERE id = ?`, util.Now(), u.ID)
		a.setSessionCookie(w, r, realm, token, sess.ExpiresAt, ch.Remember)
		a.afterLogin(u, realm, r)
		writeJSON(w, http.StatusOK, LoginResult{OK: true, User: a.userDTO(u), CSRF: sess.CSRFToken})
	}
}

func (a *App) logoutHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s, _ := sessionFromContext(r); s != nil {
			_, _ = a.db.Exec(`DELETE FROM sessions WHERE id = ?`, s.ID)
		}
		a.clearSessionCookie(w, r, realm)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) meHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u := sessionFromContext(r)
		if s == nil || u == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user":            a.userDTO(u),
			"csrf":            s.CSRFToken,
			"realm":           realm,
			"mustChangePassword": u.MustChangePassword,
			"setupCompleted":  a.settings.Bool("setup_completed"),
			"maintenance":     a.settings.Bool("maintenance_mode"),
		})
	}
}

func (a *App) changePasswordHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u := sessionFromContext(r)
		var req struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if !auth.VerifyPassword(req.CurrentPassword, u.PasswordHash) {
			writeError(w, http.StatusForbidden, "wrong_password", "")
			return
		}
		if req.CurrentPassword == req.NewPassword {
			writeError(w, http.StatusBadRequest, "password_unchanged", "")
			return
		}
		minLen := int(a.settings.Int("password_min_length"))
		if err := auth.ValidatePasswordPolicy(req.NewPassword, minLen); err != nil {
			writeError(w, http.StatusBadRequest, "weak_password", err.Error())
			return
		}
		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?`, hash, util.Now(), u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, u.ID, s.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, s.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		token, newSess, err := a.createSession(u.ID, realm, s.Remember, r.UserAgent())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		a.setSessionCookie(w, r, realm, token, newSess.ExpiresAt, s.Remember)
		a.notify(u.ID, "security", map[string]any{"event": "password_changed"})
		a.activity(u.ID, "", "password_changed", nil)
		a.audit(u.ID, u.Username, "auth.password_changed", "user", fmt.Sprint(u.ID), "success", map[string]any{"realm": realm})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "csrf": newSess.CSRFToken})
	}
}

func (a *App) listSessionsHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u := sessionFromContext(r)
		rows, err := a.db.Query(`SELECT id, realm, remember, user_agent, browser, os, created_at, last_seen_at, expires_at
			FROM sessions WHERE user_id = ? AND realm = ? AND expires_at > ? ORDER BY last_seen_at DESC`, u.ID, realm, util.Now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		defer rows.Close()
		out := []Session{}
		for rows.Next() {
			var item Session
			if err := rows.Scan(&item.ID, &item.Realm, &item.Remember, &item.UserAgent, &item.Browser, &item.OS, &item.CreatedAt, &item.LastSeenAt, &item.ExpiresAt); err != nil {
				continue
			}
			item.Current = item.ID == s.ID
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
	}
}

func (a *App) deleteSessionHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u := sessionFromContext(r)
		id := util.ParseInt(r.PathValue("id"), 0)
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		res, err := a.db.Exec(`DELETE FROM sessions WHERE id = ? AND user_id = ? AND realm = ?`, id, u.ID, realm)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "not_found", "")
			return
		}
		if id == s.ID {
			a.clearSessionCookie(w, r, realm)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) revokeOtherSessionsHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, u := sessionFromContext(r)
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND realm = ? AND id <> ?`, u.ID, realm, s.ID)
		a.audit(u.ID, u.Username, "auth.sessions_revoked", "user", fmt.Sprint(u.ID), "success", map[string]any{"realm": realm})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *App) revokeAllSessions(userID int64) {
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
}

func (a *App) totpSecret(userID int64) (string, error) {
	var enc sql.NullString
	if err := a.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ? AND totp_enabled = 1`, userID).Scan(&enc); err != nil {
		return "", err
	}
	if !enc.Valid || enc.String == "" {
		return "", errors.New("totp not configured")
	}
	raw, err := a.secrets.Open("totp", enc.String)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) consumeRecoveryCode(userID int64, code string) bool {
	if code == "" {
		return false
	}
	hash := auth.HashRecoveryCode(a.secrets.HMACKey("recovery"), code)
	res, err := a.db.Exec(`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, util.Now(), userID, hash)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (a *App) twoFASetupHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, u := sessionFromContext(r)
		if !a.settings.Bool("allow_2fa") {
			writeError(w, http.StatusForbidden, "2fa_disabled", "")
			return
		}
		if u.TOTPEnabled {
			writeError(w, http.StatusConflict, "2fa_already_enabled", "")
			return
		}
		key, err := auth.TOTPConfig(a.settings.Get("app_name"), u.Username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		png, err := qrPNG(key.URL(), 256)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"secret":     key.Secret(),
			"otpauthUrl": key.URL(),
			"qrDataUrl":  "data:image/png;base64," + png,
		})
	}
}

func (a *App) twoFAEnableHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, u := sessionFromContext(r)
		if u.TOTPEnabled {
			writeError(w, http.StatusConflict, "2fa_already_enabled", "")
			return
		}
		var req struct {
			Secret string `json:"secret"`
			Code   string `json:"code"`
		}
		if err := readJSON(r, &req); err != nil || req.Secret == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		step, ok := auth.ValidateTOTP(req.Secret, req.Code, 0)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_code", "")
			return
		}
		enc, err := a.secrets.Seal("totp", []byte(req.Secret))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		codes, hashes, err := auth.GenerateRecoveryCodes(a.secrets.HMACKey("recovery"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`UPDATE users SET totp_secret = ?, totp_enabled = 1, totp_last_step = ?, updated_at = ? WHERE id = ?`, enc, step, util.Now(), u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		for _, h := range hashes {
			if _, err := tx.Exec(`INSERT INTO recovery_codes(user_id, code_hash, created_at) VALUES(?, ?, ?)`, u.ID, h, util.Now()); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		a.notify(u.ID, "security", map[string]any{"event": "2fa_enabled"})
		a.audit(u.ID, u.Username, "auth.2fa_enabled", "user", fmt.Sprint(u.ID), "success", map[string]any{"realm": realm})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recoveryCodes": codes})
	}
}

func (a *App) twoFADisableHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, u := sessionFromContext(r)
		var req struct {
			Password string `json:"password"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if !auth.VerifyPassword(req.Password, u.PasswordHash) {
			writeError(w, http.StatusForbidden, "wrong_password", "")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`UPDATE users SET totp_secret = NULL, totp_enabled = 0, totp_last_step = 0 WHERE id = ?`, u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		a.notify(u.ID, "security", map[string]any{"event": "2fa_disabled"})
		a.audit(u.ID, u.Username, "auth.2fa_disabled", "user", fmt.Sprint(u.ID), "success", map[string]any{"realm": realm})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *App) recoveryCodesHandler(realm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, u := sessionFromContext(r)
		if !u.TOTPEnabled {
			writeError(w, http.StatusConflict, "2fa_not_enabled", "")
			return
		}
		var req struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "")
			return
		}
		if !auth.VerifyPassword(req.Password, u.PasswordHash) {
			writeError(w, http.StatusForbidden, "wrong_password", "")
			return
		}
		secret, err := a.totpSecret(u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		step, ok := auth.ValidateTOTP(secret, req.Code, u.TOTPLastStep)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_code", "")
			return
		}
		_, _ = a.db.Exec(`UPDATE users SET totp_last_step = ? WHERE id = ?`, step, u.ID)
		codes, hashes, err := auth.GenerateRecoveryCodes(a.secrets.HMACKey("recovery"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, u.ID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		for _, h := range hashes {
			if _, err := tx.Exec(`INSERT INTO recovery_codes(user_id, code_hash, created_at) VALUES(?, ?, ?)`, u.ID, h, util.Now()); err != nil {
				tx.Rollback()
				writeError(w, http.StatusInternalServerError, "internal_error", "")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		a.audit(u.ID, u.Username, "auth.recovery_codes_regenerated", "user", fmt.Sprint(u.ID), "success", nil)
		writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
	}
}

// userDTO removes secrets and adds derived fields.
func (a *App) userDTO(u *User) map[string]any {
	avatarURL := ""
	if u.AvatarPath != "" {
		avatarURL = fmt.Sprintf("/api/public/users/%d/avatar?v=%d", u.ID, u.UpdatedAt)
	}
	var prefs map[string]bool
	_ = json.Unmarshal([]byte(u.NotificationPrefs), &prefs)
	if prefs == nil {
		prefs = map[string]bool{}
	}
	return map[string]any{
		"id": u.ID, "username": u.Username, "displayName": u.DisplayName, "role": u.Role,
		"mustChangePassword": u.MustChangePassword, "suspended": u.Suspended,
		"namespace": u.Namespace, "storageLimitBytes": u.StorageLimitBytes,
		"storageUsedBytes": u.StorageUsedBytes, "storageReservedBytes": u.StorageReservedBytes,
		"totpEnabled": u.TOTPEnabled, "language": u.Language, "theme": u.Theme,
		"notificationPrefs": prefs, "avatarUrl": avatarURL, "createdAt": u.CreatedAt,
		"lastLoginAt": u.LastLoginAt, "suspendedReason": u.SuspendedReason,
	}
}
