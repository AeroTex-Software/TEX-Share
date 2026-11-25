package app

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

	texweb "github.com/AeroTex-Software/TEX-Share/web"
)

// PublicHandler returns the complete public HTTP handler (API + SPA).
func (a *App) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	a.registerHealth(mux)

	// Public metadata and assets.
	mux.HandleFunc("GET /api/public/meta", a.publicMetaHandler)
	mux.HandleFunc("GET /api/public/branding/{kind}", a.publicBrandingHandler)
	mux.HandleFunc("GET /api/public/users/{id}/avatar", a.avatarHandler)

	// Public authentication.
	mux.HandleFunc("POST /api/public/auth/login", a.loginHandler("public"))
	mux.HandleFunc("POST /api/public/auth/2fa", a.verifyTOTPHandler("public"))
	mux.HandleFunc("POST /api/public/auth/logout", a.requireUser("public", a.logoutHandler("public")))
	mux.HandleFunc("GET /api/public/auth/me", a.requireUser("public", a.meHandler("public")))
	mux.HandleFunc("POST /api/public/auth/change-password", a.requireUser("public", a.changePasswordHandler("public")))
	mux.HandleFunc("GET /api/public/auth/sessions", a.requireUser("public", a.listSessionsHandler("public")))
	mux.HandleFunc("DELETE /api/public/auth/sessions/{id}", a.requireUser("public", a.deleteSessionHandler("public")))
	mux.HandleFunc("POST /api/public/auth/sessions/revoke-others", a.requireUser("public", a.revokeOtherSessionsHandler("public")))
	mux.HandleFunc("POST /api/public/auth/2fa/setup", a.requireUser("public", a.twoFASetupHandler("public")))
	mux.HandleFunc("POST /api/public/auth/2fa/enable", a.requireUser("public", a.twoFAEnableHandler("public")))
	mux.HandleFunc("POST /api/public/auth/2fa/disable", a.requireUser("public", a.twoFADisableHandler("public")))
	mux.HandleFunc("POST /api/public/auth/recovery-codes", a.requireUser("public", a.recoveryCodesHandler("public")))

	// Account, notifications and activity.
	mux.HandleFunc("GET /api/public/config", a.requireUser("public", a.publicConfigHandler))
	mux.HandleFunc("GET /api/public/profile", a.requireUser("public", a.profileHandler))
	mux.HandleFunc("PATCH /api/public/profile", a.requireUser("public", a.updateProfileHandler))
	mux.HandleFunc("POST /api/public/profile/avatar", a.requireUser("public", a.uploadAvatarHandler))
	mux.HandleFunc("DELETE /api/public/profile/avatar", a.requireUser("public", a.deleteAvatarHandler))
	mux.HandleFunc("GET /api/public/notifications", a.requireUser("public", a.notificationsHandler))
	mux.HandleFunc("POST /api/public/notifications/read", a.requireUser("public", a.readNotificationsHandler))
	mux.HandleFunc("DELETE /api/public/notifications", a.requireUser("public", a.deleteNotificationsHandler))
	mux.HandleFunc("GET /api/public/activity", a.requireUser("public", a.activityHandler))
	mux.HandleFunc("GET /api/public/stats", a.requireUser("public", a.userStatsHandler))
	mux.HandleFunc("GET /api/public/domains", a.requireUser("public", a.publicDomainsHandler))

	// Share management for normal users.
	mux.HandleFunc("GET /api/public/shares", a.requireUser("public", a.listSharesHandler(false)))
	mux.HandleFunc("POST /api/public/shares", a.requireUser("public", a.createShareHandler))
	mux.HandleFunc("GET /api/public/shares/{id}", a.requireUser("public", a.getShareHandler))
	mux.HandleFunc("PATCH /api/public/shares/{id}", a.requireUser("public", a.patchShareHandler))
	mux.HandleFunc("DELETE /api/public/shares/{id}", a.requireUser("public", a.trashShareHandler))
	mux.HandleFunc("POST /api/public/shares/{id}/restore", a.requireUser("public", a.restoreShareHandler))
	mux.HandleFunc("DELETE /api/public/shares/{id}/purge", a.requireUser("public", a.purgeShareHandler))
	mux.HandleFunc("POST /api/public/shares/{id}/rotate-token", a.requireUser("public", a.rotateTokenHandler))
	mux.HandleFunc("POST /api/public/shares/{id}/rotate-key", a.requireUser("public", a.rotateKeyHandler))
	mux.HandleFunc("GET /api/public/shares/{id}/stats", a.requireUser("public", a.shareStatsHandler))
	// Owner access to raw E2EE ciphertext for client-side key rotation. This
	// never counts as a download and never exposes plaintext.
	mux.HandleFunc("GET /api/public/shares/{id}/e2ee/{fileId}", a.requireUser("public", a.ownerE2EEBlobHandler))

	// Resumable/direct uploads.
	mux.HandleFunc("POST /api/public/uploads", a.requireUser("public", a.uploadInitHandler))
	mux.HandleFunc("PUT /api/public/uploads/{id}", a.requireUser("public", a.directUploadHandler))
	mux.HandleFunc("PUT /api/public/uploads/{id}/chunks/{index}", a.requireUser("public", a.chunkUploadHandler))
	mux.HandleFunc("GET /api/public/uploads/{id}", a.requireUser("public", a.uploadStatusHandler))
	mux.HandleFunc("POST /api/public/uploads/{id}/complete", a.requireUser("public", a.completeUploadHandler))
	mux.HandleFunc("DELETE /api/public/uploads/{id}", a.requireUser("public", a.cancelUploadHandler))

	// Public share access (token based, optionally password protected).
	mux.HandleFunc("GET /api/public/share/{token}", a.publicShareHandler)
	mux.HandleFunc("POST /api/public/share/{token}/unlock", a.unlockShareHandler)
	mux.HandleFunc("POST /api/public/share/{token}/view", a.viewShareHandler)
	mux.HandleFunc("GET /api/public/share/{token}/files/{fileId}/download", a.shareFileDownloadHandler(false))
	mux.HandleFunc("GET /api/public/share/{token}/files/{fileId}/inline", a.shareFileDownloadHandler(true))
	mux.HandleFunc("GET /api/public/share/{token}/e2ee/{fileId}", a.e2eeBlobHandler)
	mux.HandleFunc("GET /api/public/share/{token}/zip", a.zipDownloadHandler)
	mux.HandleFunc("POST /api/public/share/{token}/e2ee-zip/start", a.e2eeZipStartHandler)
	mux.HandleFunc("POST /api/public/share/{token}/e2ee-zip/complete", a.e2eeZipCompleteHandler)

	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "")
	}))
	mux.Handle("/", a.staticHandler(false))
	return a.publicSetupGate(a.maintenanceGate(a.apiRateLimit(a.commonMiddleware(mux, false))))
}

// AdminHandler returns the complete admin HTTP handler. It is served on the
// separate admin port and only accepts admin sessions.
func (a *App) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	a.registerHealth(mux)

	mux.HandleFunc("GET /api/admin/setup/status", a.setupStatusHandler)
	mux.HandleFunc("POST /api/admin/setup", a.setupHandler)

	mux.HandleFunc("POST /api/admin/auth/login", a.loginHandler("admin"))
	mux.HandleFunc("POST /api/admin/auth/2fa", a.verifyTOTPHandler("admin"))
	mux.HandleFunc("POST /api/admin/auth/logout", a.requireUser("admin", a.logoutHandler("admin")))
	mux.HandleFunc("GET /api/admin/auth/me", a.requireUser("admin", a.meHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/change-password", a.requireUser("admin", a.changePasswordHandler("admin")))
	mux.HandleFunc("GET /api/admin/auth/sessions", a.requireUser("admin", a.listSessionsHandler("admin")))
	mux.HandleFunc("DELETE /api/admin/auth/sessions/{id}", a.requireUser("admin", a.deleteSessionHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/sessions/revoke-others", a.requireUser("admin", a.revokeOtherSessionsHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/2fa/setup", a.requireUser("admin", a.twoFASetupHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/2fa/enable", a.requireUser("admin", a.twoFAEnableHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/2fa/disable", a.requireUser("admin", a.twoFADisableHandler("admin")))
	mux.HandleFunc("POST /api/admin/auth/recovery-codes", a.requireUser("admin", a.recoveryCodesHandler("admin")))

	mux.HandleFunc("GET /api/admin/dashboard", a.requireUser("admin", a.dashboardHandler))
	mux.HandleFunc("GET /api/admin/monitoring", a.requireUser("admin", a.monitoringHandler))
	mux.HandleFunc("GET /api/admin/stats", a.requireUser("admin", a.adminGlobalStatsHandler))

	mux.HandleFunc("GET /api/admin/profile", a.requireUser("admin", a.profileHandler))
	mux.HandleFunc("PATCH /api/admin/profile", a.requireUser("admin", a.updateProfileHandler))
	mux.HandleFunc("POST /api/admin/profile/avatar", a.requireUser("admin", a.uploadAvatarHandler))
	mux.HandleFunc("DELETE /api/admin/profile/avatar", a.requireUser("admin", a.deleteAvatarHandler))
	mux.HandleFunc("GET /api/admin/notifications", a.requireUser("admin", a.notificationsHandler))
	mux.HandleFunc("POST /api/admin/notifications/read", a.requireUser("admin", a.readNotificationsHandler))
	mux.HandleFunc("DELETE /api/admin/notifications", a.requireUser("admin", a.deleteNotificationsHandler))
	mux.HandleFunc("GET /api/admin/activity", a.requireUser("admin", a.activityHandler))

	mux.HandleFunc("GET /api/admin/users", a.requireUser("admin", a.adminListUsersHandler))
	mux.HandleFunc("POST /api/admin/users", a.requireUser("admin", a.adminCreateUserHandler))
	mux.HandleFunc("GET /api/admin/users/{id}", a.requireUser("admin", a.adminGetUserHandler))
	mux.HandleFunc("PATCH /api/admin/users/{id}", a.requireUser("admin", a.adminPatchUserHandler))
	mux.HandleFunc("DELETE /api/admin/users/{id}", a.requireUser("admin", a.adminDeleteUserHandler))
	mux.HandleFunc("GET /api/admin/users/{id}/delete-preview", a.requireUser("admin", a.adminDeletePreviewHandler))
	mux.HandleFunc("POST /api/admin/users/{id}/suspend", a.requireUser("admin", a.adminSuspendUserHandler))
	mux.HandleFunc("POST /api/admin/users/{id}/reactivate", a.requireUser("admin", a.adminReactivateUserHandler))
	mux.HandleFunc("POST /api/admin/users/{id}/reset-password", a.requireUser("admin", a.adminResetPasswordHandler))
	mux.HandleFunc("POST /api/admin/users/{id}/reset-2fa", a.requireUser("admin", a.adminReset2FAHandler))
	mux.HandleFunc("POST /api/admin/users/{id}/revoke-sessions", a.requireUser("admin", a.adminRevokeSessionsHandler))

	mux.HandleFunc("GET /api/admin/domains", a.requireUser("admin", a.adminListDomainsHandler))
	mux.HandleFunc("POST /api/admin/domains", a.requireUser("admin", a.adminCreateDomainHandler))
	mux.HandleFunc("PATCH /api/admin/domains/{id}", a.requireUser("admin", a.adminPatchDomainHandler))
	mux.HandleFunc("DELETE /api/admin/domains/{id}", a.requireUser("admin", a.adminDeleteDomainHandler))
	mux.HandleFunc("POST /api/admin/domains/{id}/verify", a.requireUser("admin", a.adminVerifyDomainHandler))
	mux.HandleFunc("POST /api/admin/domains/{id}/check", a.requireUser("admin", a.adminCheckDomainHandler))

	mux.HandleFunc("GET /api/admin/shares", a.requireUser("admin", a.listSharesHandler(true)))
	mux.HandleFunc("GET /api/admin/shares/{id}", a.requireUser("admin", a.adminGetShareHandler))
	mux.HandleFunc("PATCH /api/admin/shares/{id}", a.requireUser("admin", a.adminPatchShareHandler))
	mux.HandleFunc("DELETE /api/admin/shares/{id}", a.requireUser("admin", a.adminTrashShareHandler))
	mux.HandleFunc("POST /api/admin/shares/{id}/restore", a.requireUser("admin", a.adminRestoreShareHandler))
	mux.HandleFunc("DELETE /api/admin/shares/{id}/purge", a.requireUser("admin", a.adminPurgeShareHandler))
	mux.HandleFunc("POST /api/admin/shares/{id}/rotate-token", a.requireUser("admin", a.adminRotateTokenHandler))
	mux.HandleFunc("GET /api/admin/shares/{id}/content", a.requireUser("admin", a.adminShareContentHandler))
	mux.HandleFunc("GET /api/admin/shares/{id}/files/{fileId}/download", a.requireUser("admin", a.adminDownloadFileHandler))

	// Media aliases so the admin SPA can render branding and avatars from its
	// own origin without depending on the public listener.
	mux.HandleFunc("GET /api/admin/branding/{kind}", a.publicBrandingHandler)
	mux.HandleFunc("GET /api/admin/users/{id}/avatar", a.avatarHandler)

	mux.HandleFunc("GET /api/admin/settings", a.requireUser("admin", a.adminSettingsHandler))
	mux.HandleFunc("PATCH /api/admin/settings", a.requireUser("admin", a.adminPatchSettingsHandler))
	mux.HandleFunc("POST /api/admin/branding/{kind}", a.requireUser("admin", a.adminUploadBrandingHandler))
	mux.HandleFunc("DELETE /api/admin/branding/{kind}", a.requireUser("admin", a.adminDeleteBrandingHandler))

	mux.HandleFunc("GET /api/admin/audit", a.requireUser("admin", a.auditListHandler))
	mux.HandleFunc("GET /api/admin/audit/verify", a.requireUser("admin", a.auditVerifyHandler))
	mux.HandleFunc("GET /api/admin/audit/export", a.requireUser("admin", a.auditExportHandler))

	mux.HandleFunc("GET /api/admin/backups", a.requireUser("admin", a.listBackupsHandler))
	mux.HandleFunc("POST /api/admin/backups", a.requireUser("admin", a.createBackupHandler))
	mux.HandleFunc("POST /api/admin/backups/restore", a.requireUser("admin", a.restoreBackupHandler))
	mux.HandleFunc("POST /api/admin/backups/prune", a.requireUser("admin", a.pruneBackupsHandler))
	mux.HandleFunc("GET /api/admin/backups/{id}/download", a.requireUser("admin", a.downloadBackupHandler))
	mux.HandleFunc("DELETE /api/admin/backups/{id}", a.requireUser("admin", a.deleteBackupHandler))

	mux.HandleFunc("GET /api/admin/cloudflare", a.requireUser("admin", a.cloudflareStatusHandler))
	mux.HandleFunc("POST /api/admin/cloudflare/login", a.requireUser("admin", a.cloudflareLoginHandler))
	mux.HandleFunc("POST /api/admin/cloudflare/cancel", a.requireUser("admin", a.cloudflareCancelHandler))
	mux.HandleFunc("POST /api/admin/cloudflare/provision", a.requireUser("admin", a.cloudflareProvisionHandler))
	mux.HandleFunc("POST /api/admin/cloudflare/disable", a.requireUser("admin", a.cloudflareDisableHandler))
	mux.HandleFunc("POST /api/admin/cloudflare/disconnect", a.requireUser("admin", a.cloudflareDisconnectHandler))

	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "")
	}))
	mux.Handle("/", a.staticHandler(true))
	return a.adminSetupGate(a.apiRateLimit(a.commonMiddleware(mux, true)))
}

func (a *App) registerHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "texshare", "version": a.cfg.Version})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.PingContext(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// staticHandler serves the embedded SPA. The public and admin bundles are
// separate HTML entry points; unknown non-API paths fall back to the SPA.
func (a *App) staticHandler(admin bool) http.Handler {
	index := "public.html"
	if admin {
		index = "admin.html"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(clean, "/")
		if strings.HasPrefix(name, "assets/") {
			data, err := fs.ReadFile(texweb.Dist(), name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			ct := mime.TypeByExtension(path.Ext(name))
			if ct == "" {
				ct = http.DetectContentType(data)
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(data)
			return
		}
		if name == "favicon.ico" || name == "favicon.png" {
			if a.serveBrandingFavicon(w, r) {
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		data, err := fs.ReadFile(texweb.Dist(), index)
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}

func (a *App) serveBrandingFavicon(w http.ResponseWriter, r *http.Request) bool {
	name := a.settings.Get("favicon_path")
	if name == "" {
		return false
	}
	data, err := os.ReadFile(path.Join(a.cfg.DataDir, "branding", path.Base(name)))
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
	return true
}
