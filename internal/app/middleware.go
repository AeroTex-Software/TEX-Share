package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytes += int64(n)
	return n, err
}

// Unwrap enables http.ResponseController / Flush on the wrapped writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (a *App) commonMiddleware(next http.Handler, admin bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Restore replaces the database and storage directories. Every other
		// request holds a read lock; the restore endpoint itself is exempt and
		// takes the write lock inside its handler.
		if r.URL.Path != "/api/admin/backups/restore" {
			if !a.restoreMu.TryRLock() {
				writeError(w, http.StatusServiceUnavailable, "restore_in_progress", "")
				return
			}
			defer a.restoreMu.RUnlock()
		}
		rec := &statusRecorder{ResponseWriter: w}
		a.setSecurityHeaders(rec, r, admin)
		if !a.hostAllowed(r, admin) {
			writeError(rec, http.StatusMisdirectedRequest, "invalid_host", "")
			a.log.Warn("request rejected: invalid host", "host", hostOnly(r.Host))
			return
		}
		defer func() {
			if v := recover(); v != nil {
				atomic.AddInt64(&a.errorCount, 1)
				a.log.Error("panic in request", "route", r.Pattern, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if rec.status == 0 {
					writeError(rec, http.StatusInternalServerError, "internal_error", "")
				}
			}
			if a.cfg.AccessLog && !strings.HasPrefix(r.URL.Path, "/assets/") {
				route := r.Pattern
				if route == "" {
					route = "unmatched"
				}
				status := rec.status
				if status == 0 {
					status = http.StatusOK
				}
				a.log.Info("request",
					"method", r.Method,
					"route", route,
					"status", status,
					"bytes", rec.bytes,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func (a *App) setSecurityHeaders(w http.ResponseWriter, r *http.Request, admin bool) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self' 'wasm-unsafe-eval'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: blob:; "+
			"media-src 'self' blob:; "+
			"font-src 'self' data:; "+
			"connect-src 'self'; "+
			"worker-src 'self' blob:; "+
			"frame-src 'self' blob:; "+
			"object-src 'none'; "+
			"base-uri 'self'; "+
			"form-action 'self'; "+
			"frame-ancestors 'none'")
	if r.TLS != nil || a.cfg.ForceSecure {
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store, private")
		h.Set("Pragma", "no-cache")
	}
	if admin {
		h.Set("X-TEXShare-Admin", "1")
	}
}

// apiRateLimit applies a per-IP token bucket to API calls.
func (a *App) apiRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := a.clientIP(r)
		rate := float64(a.settings.Int("api_rate_per_minute"))
		if rate <= 0 {
			rate = 300
		}
		if !a.apiLimiter.Allow("api:"+ip, rate, rate*2) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// publicSetupGate blocks public functionality before initial setup.
func (a *App) publicSetupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.setupComplete() {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/public/meta" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "setup_required", "")
	})
}

// adminSetupGate only exposes setup and health endpoints before initialization.
func (a *App) adminSetupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.setupComplete() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/api/admin/setup") || p == "/healthz" || p == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "setup_required", "")
	})
}

// maintenanceGate keeps public share access alive while disabling the normal UI.
func (a *App) maintenanceGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.settings.Bool("maintenance_mode") {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") || p == "/api/public/meta" || strings.HasPrefix(p, "/api/public/share/") ||
			p == "/healthz" || p == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "maintenance", "")
	})
}

func (a *App) logRequestError(r *http.Request, msg string, err error) {
	a.log.Error(msg, "route", r.Pattern, "error", slog.StringValue(err.Error()))
}
