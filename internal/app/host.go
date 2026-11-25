package app

import (
	"database/sql"
	"net"
	"net/http"
	"strings"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) refreshHostCache() {
	public := map[string]bool{}
	for _, h := range a.cfg.AllowedHosts {
		public[h] = true
	}
	rows, err := a.db.Query(`SELECT hostname, enabled FROM domains`)
	if err == nil {
		for rows.Next() {
			var host string
			var enabled int
			if rows.Scan(&host, &enabled) == nil && enabled == 1 {
				public[strings.ToLower(host)] = true
			}
		}
		rows.Close()
	}
	a.hostMu.Lock()
	a.publicHosts = public
	admin := map[string]bool{}
	for _, h := range a.cfg.AdminHosts {
		admin[h] = true
	}
	a.adminHosts = admin
	a.hostMu.Unlock()
}

func hostOnly(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.ToLower(host)
}

func (a *App) hostAllowed(r *http.Request, admin bool) bool {
	if a.cfg.Dev {
		return true
	}
	host := hostOnly(r.Host)
	if host == "" {
		return false
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if net.ParseIP(host) != nil {
		return true
	}
	a.hostMu.RLock()
	defer a.hostMu.RUnlock()
	if admin && len(a.adminHosts) > 0 {
		if a.adminHosts[host] {
			return true
		}
		// Admin may be served from the same hostname as the public UI.
		return a.publicHosts[host]
	}
	return a.publicHosts[host]
}

// setupComplete reports whether an administrator exists and setup is closed.
func (a *App) setupComplete() bool {
	if a.settings.Bool("setup_completed") {
		return true
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (a *App) ensureSetupFlag() {
	if a.setupComplete() {
		if !a.settings.Bool("setup_completed") {
			_ = a.settings.Set("setup_completed", "1")
		}
	}
}

// countAdmins is used by setup and deletion safeguards.
func (a *App) countAdmins() int {
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n
}

func (a *App) firstDomain() (*Domain, error) {
	var id int64
	err := a.db.QueryRow(`SELECT id FROM domains ORDER BY is_default DESC, id ASC LIMIT 1`).Scan(&id)
	if err != nil {
		return nil, err
	}
	return a.domainByID(id)
}

func (a *App) defaultDomain() (*Domain, error) {
	var id int64
	err := a.db.QueryRow(`SELECT id FROM domains WHERE is_default = 1 LIMIT 1`).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return a.firstDomain()
		}
		return nil, err
	}
	return a.domainByID(id)
}

// userDomainAllowed checks explicit grants. Administrators may use all domains.
// A user without any explicit grant is allowed every enabled domain, so that a
// freshly created account can share immediately; assigning specific domains
// restricts the user to exactly those.
func (a *App) userDomainAllowed(u *User, domainID int64) bool {
	if u.Role == "admin" {
		return true
	}
	var grants int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM user_domains WHERE user_id = ?`, u.ID).Scan(&grants)
	if grants == 0 {
		return true
	}
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM user_domains WHERE user_id = ? AND domain_id = ?`, u.ID, domainID).Scan(&n)
	return n > 0
}

func (a *App) allowedDomainsFor(u *User) ([]Domain, error) {
	query := `SELECT d.id, d.hostname, d.enabled, d.verified, d.verification_token, d.is_default, d.tls_status,
		COALESCE(d.last_checked_at,0), d.last_check_ok, COALESCE(d.last_check_error,''), d.created_at
		FROM domains d`
	args := []any{}
	if u.Role != "admin" {
		// No explicit grants means every enabled domain is available.
		var grants int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM user_domains WHERE user_id = ?`, u.ID).Scan(&grants)
		if grants > 0 {
			query += ` JOIN user_domains ud ON ud.domain_id = d.id AND ud.user_id = ?`
			args = append(args, u.ID)
		}
	}
	query += ` WHERE d.enabled = 1 ORDER BY d.is_default DESC, d.hostname ASC`
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		var lastOK sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Enabled, &d.Verified, &d.VerificationToken, &d.IsDefault,
			&d.TLSStatus, &d.LastCheckedAt, &lastOK, &d.LastCheckError, &d.CreatedAt); err != nil {
			continue
		}
		if lastOK.Valid {
			v := lastOK.Int64 == 1
			d.LastCheckOK = &v
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// validShareHost verifies that the request host is the domain configured for a
// share. In development this check is relaxed for local testing.
func (a *App) validShareHost(r *http.Request, domain *Domain) bool {
	if a.cfg.Dev {
		return true
	}
	return strings.EqualFold(hostOnly(r.Host), strings.ToLower(domain.Hostname))
}

var _ = util.Now
