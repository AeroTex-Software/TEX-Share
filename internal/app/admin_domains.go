package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) adminListDomainsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT d.id, d.hostname, d.enabled, d.verified, d.verification_token, d.is_default,
		d.tls_status, COALESCE(d.last_checked_at,0), d.last_check_ok, COALESCE(d.last_check_error,''), d.created_at,
		(SELECT COUNT(*) FROM user_domains ud WHERE ud.domain_id = d.id),
		(SELECT COUNT(*) FROM shares s WHERE s.domain_id = d.id AND s.status NOT IN ('deleted','expired'))
		FROM domains d ORDER BY d.is_default DESC, d.hostname ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		var lastOK interface{}
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Enabled, &d.Verified, &d.VerificationToken, &d.IsDefault,
			&d.TLSStatus, &d.LastCheckedAt, &lastOK, &d.LastCheckError, &d.CreatedAt, &d.UserCount, &d.ShareCount); err != nil {
			continue
		}
		if b, ok := lastOK.(int64); ok {
			v := b == 1
			d.LastCheckOK = &v
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

func (a *App) adminCreateDomainHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	var req struct {
		Hostname  string `json:"hostname"`
		Enabled   *bool  `json:"enabled"`
		IsDefault bool   `json:"isDefault"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	host := strings.ToLower(strings.TrimSpace(req.Hostname))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	if !util.ValidHostname(host) {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if req.IsDefault {
		if _, err := tx.Exec(`UPDATE domains SET is_default = 0`); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	}
	res, err := tx.Exec(`INSERT INTO domains(hostname, enabled, verified, verification_token, is_default, tls_status, created_at)
		VALUES(?, ?, 0, ?, ?, 'unknown', ?)`, host, boolInt(enabled), util.RandomHex(16), boolInt(req.IsDefault), util.Now())
	if err != nil {
		tx.Rollback()
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "domain_exists", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	id, _ := res.LastInsertId()
	if req.IsDefault {
		_, _ = tx.Exec(`INSERT INTO settings(key, value) VALUES('default_domain_id', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(id))
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if req.IsDefault {
		_ = a.settings.Set("default_domain_id", fmt.Sprint(id))
	}
	a.refreshHostCache()
	a.audit(actor.ID, actor.Username, "domain.create", "domain", fmt.Sprint(id), "success", map[string]any{"hostname": host})
	d, _ := a.domainByID(id)
	writeJSON(w, http.StatusCreated, map[string]any{"domain": d})
}

func (a *App) adminPatchDomainHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	var req struct {
		Hostname  *string `json:"hostname"`
		Enabled   *bool   `json:"enabled"`
		IsDefault *bool   `json:"isDefault"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	sets := []string{}
	args := []any{}
	if req.Hostname != nil {
		host := strings.ToLower(strings.TrimSpace(*req.Hostname))
		host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
		host = strings.TrimRight(host, "/")
		if !util.ValidHostname(host) {
			tx.Rollback()
			writeError(w, http.StatusBadRequest, "invalid_hostname", "")
			return
		}
		if host != d.Hostname {
			sets = append(sets, "hostname = ?", "verified = 0", "verification_token = ?")
			args = append(args, host, util.RandomHex(16))
		}
	}
	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolInt(*req.Enabled))
	}
	if req.IsDefault != nil && *req.IsDefault {
		if _, err := tx.Exec(`UPDATE domains SET is_default = 0`); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		sets = append(sets, "is_default = 1")
		_, _ = tx.Exec(`INSERT INTO settings(key, value) VALUES('default_domain_id', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(id))
		_ = a.settings.Set("default_domain_id", fmt.Sprint(id))
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := tx.Exec(`UPDATE domains SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
			tx.Rollback()
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				writeError(w, http.StatusConflict, "domain_exists", "")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.refreshHostCache()
	a.audit(actor.ID, actor.Username, "domain.update", "domain", fmt.Sprint(id), "success", nil)
	updated, _ := a.domainByID(id)
	writeJSON(w, http.StatusOK, map[string]any{"domain": updated})
}

func (a *App) adminDeleteDomainHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	var shares int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE domain_id = ?`, id).Scan(&shares)
	if shares > 0 {
		writeError(w, http.StatusConflict, "domain_in_use", "")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if _, err := tx.Exec(`DELETE FROM domains WHERE id = ?`, id); err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	if d.IsDefault {
		var next int64
		if err := tx.QueryRow(`SELECT id FROM domains ORDER BY id LIMIT 1`).Scan(&next); err == nil && next > 0 {
			_, _ = tx.Exec(`UPDATE domains SET is_default = 1 WHERE id = ?`, next)
			_, _ = tx.Exec(`INSERT INTO settings(key, value) VALUES('default_domain_id', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(next))
			_ = a.settings.Set("default_domain_id", fmt.Sprint(next))
		} else {
			_ = a.settings.Set("default_domain_id", "0")
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.refreshHostCache()
	a.audit(actor.ID, actor.Username, "domain.delete", "domain", fmt.Sprint(id), "success", map[string]any{"hostname": d.Hostname})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminVerifyDomainHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	resolver := net.DefaultResolver
	txts, txtErr := resolver.LookupTXT(ctx, "_texshare-verify."+d.Hostname)
	verified := false
	for _, t := range txts {
		if util.ConstantEqual(strings.TrimSpace(t), d.VerificationToken) {
			verified = true
		}
	}
	tlsStatus, reachable, checkErr := checkDomain(ctx, d.Hostname)
	lastOK := verified || reachable
	errText := ""
	if txtErr != nil {
		errText = "TXT lookup: " + txtErr.Error()
	}
	if checkErr != nil {
		if errText != "" {
			errText += "; "
		}
		errText += checkErr.Error()
	}
	_, _ = a.db.Exec(`UPDATE domains SET verified = ?, tls_status = ?, last_checked_at = ?, last_check_ok = ?, last_check_error = ? WHERE id = ?`,
		boolInt(verified), tlsStatus, util.Now(), boolInt(lastOK), nullString(errText), id)
	a.audit(actor.ID, actor.Username, "domain.verify", "domain", fmt.Sprint(id), "success", map[string]any{"hostname": d.Hostname, "verified": verified, "reachable": reachable})
	updated, _ := a.domainByID(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": updated, "dnsVerified": verified, "reachable": reachable, "tlsStatus": tlsStatus,
		"txtRecord": "_texshare-verify." + d.Hostname, "txtValue": d.VerificationToken,
	})
}

func (a *App) adminCheckDomainHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	tlsStatus, reachable, checkErr := checkDomain(ctx, d.Hostname)
	errText := ""
	if checkErr != nil {
		errText = checkErr.Error()
	}
	_, _ = a.db.Exec(`UPDATE domains SET tls_status = ?, last_checked_at = ?, last_check_ok = ?, last_check_error = ? WHERE id = ?`,
		tlsStatus, util.Now(), boolInt(reachable), nullString(errText), id)
	updated, _ := a.domainByID(id)
	writeJSON(w, http.StatusOK, map[string]any{"domain": updated, "reachable": reachable, "tlsStatus": tlsStatus, "error": errText})
}

// checkDomain performs a safe reachability probe. Private, loopback and
// link-local addresses are rejected to prevent SSRF through admin-supplied DNS.
func checkDomain(ctx context.Context, hostname string) (tlsStatus string, reachable bool, err error) {
	if !util.ValidHostname(hostname) {
		return "unknown", false, fmt.Errorf("invalid hostname")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: safeDialControl}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("https://" + hostname + "/healthz")
	if err != nil {
		// Try plain HTTP only to report a missing TLS setup.
		resp2, err2 := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			DialContext:       dialer.DialContext,
			DisableKeepAlives: true,
		}}).Get("http://" + hostname + "/healthz")
		if err2 != nil {
			return "unknown", false, err
		}
		defer resp2.Body.Close()
		if resp2.StatusCode == http.StatusOK {
			return "missing", true, fmt.Errorf("HTTPS unavailable")
		}
		return "missing", false, fmt.Errorf("HTTP status %d", resp2.StatusCode)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.TLS != nil {
		tlsStatus = "valid"
	} else {
		tlsStatus = "missing"
	}
	if resp.StatusCode != http.StatusOK {
		return tlsStatus, false, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	var probe struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Service != "texshare" {
		return tlsStatus, false, fmt.Errorf("not a TEX Share instance")
	}
	return tlsStatus, true, nil
}

func safeDialControl(network, address string, c syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("unresolvable address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("private address blocked")
	}
	return nil
}
