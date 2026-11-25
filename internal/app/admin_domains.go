package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/network/proxy"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

func (a *App) adminListDomainsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT d.id, d.hostname, d.enabled, d.verified, d.verification_token, d.is_default,
		d.tls_status, COALESCE(d.last_checked_at,0), d.last_check_ok, COALESCE(d.last_check_error,''), d.created_at,
		d.dns_provider, d.dns_record_type, d.dns_target, d.dns_zone_id, d.dns_record_id, d.dns_status,
		COALESCE(d.dns_checked_at,0), d.dns_ok, COALESCE(d.dns_error,''), d.network_mode, d.proxy_kind,
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
		var lastOK, dnsOK interface{}
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Enabled, &d.Verified, &d.VerificationToken, &d.IsDefault,
			&d.TLSStatus, &d.LastCheckedAt, &lastOK, &d.LastCheckError, &d.CreatedAt,
			&d.DNSProvider, &d.DNSRecordType, &d.DNSTarget, &d.DNSZoneID, &d.DNSRecordID, &d.DNSStatus,
			&d.DNSCheckedAt, &dnsOK, &d.DNSError, &d.NetworkMode, &d.ProxyKind, &d.UserCount, &d.ShareCount); err != nil {
			continue
		}
		if b, ok := lastOK.(int64); ok {
			v := b == 1
			d.LastCheckOK = &v
		}
		if b, ok := dnsOK.(int64); ok {
			v := b == 1
			d.DNSOK = &v
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

// domainBinding is the optional DNS/network binding accepted on create/patch.
type domainBinding struct {
	DNSProvider   *string `json:"dnsProvider"`
	DNSRecordType *string `json:"dnsRecordType"`
	DNSTarget     *string `json:"dnsTarget"`
	NetworkMode   *string `json:"networkMode"`
	ProxyKind     *string `json:"proxyKind"`
}

var validNetworkModes = map[string]bool{"": true, "direct": true, "proxy": true, "tunnel": true, "static": true, "ddns": true}
var validRecordTypes = map[string]bool{"": true, "A": true, "AAAA": true, "CNAME": true}

func (a *App) adminCreateDomainHandler(w http.ResponseWriter, r *http.Request) {
	actor := userFromContext(r)
	var req struct {
		Hostname  string `json:"hostname"`
		Enabled   *bool  `json:"enabled"`
		IsDefault bool   `json:"isDefault"`
		domainBinding
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
	binding, berr := a.normalizeBinding(req.domainBinding)
	if berr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", berr.Error())
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
	res, err := tx.Exec(`INSERT INTO domains(hostname, enabled, verified, verification_token, is_default, tls_status, created_at,
		dns_provider, dns_record_type, dns_target, network_mode, proxy_kind)
		VALUES(?, ?, 0, ?, ?, 'unknown', ?, ?, ?, ?, ?, ?)`,
		host, boolInt(enabled), util.RandomHex(16), boolInt(req.IsDefault), util.Now(),
		binding.dnsProvider, binding.dnsRecordType, binding.dnsTarget, binding.networkMode, binding.proxyKind)
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
	writeJSON(w, http.StatusCreated, map[string]any{"domain": d, "instructions": a.manualInstructions(d)})
}

type normalizedBinding struct {
	dnsProvider   string
	dnsRecordType string
	dnsTarget     string
	networkMode   string
	proxyKind     string
}

func (a *App) normalizeBinding(b domainBinding) (normalizedBinding, error) {
	out := normalizedBinding{}
	if b.DNSProvider != nil {
		v := strings.ToLower(strings.TrimSpace(*b.DNSProvider))
		if v != "" && !providerSupportsAPI(v) && v != "manual" && v != "none" {
			return out, fmt.Errorf("unknown dns provider %q", v)
		}
		out.dnsProvider = v
	}
	if b.DNSRecordType != nil {
		v := strings.ToUpper(strings.TrimSpace(*b.DNSRecordType))
		if !validRecordTypes[v] {
			return out, fmt.Errorf("invalid record type %q", v)
		}
		out.dnsRecordType = v
	}
	if b.DNSTarget != nil {
		out.dnsTarget = strings.TrimSpace(*b.DNSTarget)
	}
	if b.NetworkMode != nil {
		v := strings.ToLower(strings.TrimSpace(*b.NetworkMode))
		if !validNetworkModes[v] {
			return out, fmt.Errorf("invalid network mode %q", v)
		}
		out.networkMode = v
	}
	if b.ProxyKind != nil {
		v := strings.ToLower(strings.TrimSpace(*b.ProxyKind))
		if v != "" && !proxy.ValidKind(v) {
			return out, fmt.Errorf("invalid proxy kind %q", v)
		}
		out.proxyKind = v
	}
	return out, nil
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
		domainBinding
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	binding, berr := a.normalizeBinding(req.domainBinding)
	if berr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", berr.Error())
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
			// Changing the hostname invalidates verification and any managed record.
			sets = append(sets, "hostname = ?", "verified = 0", "verification_token = ?", "dns_record_id = ''", "dns_status = 'unknown'")
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
	if req.DNSProvider != nil {
		sets = append(sets, "dns_provider = ?", "dns_record_id = ''", "dns_status = 'unknown'")
		args = append(args, binding.dnsProvider)
	}
	if req.DNSRecordType != nil {
		sets = append(sets, "dns_record_type = ?")
		args = append(args, binding.dnsRecordType)
	}
	if req.DNSTarget != nil {
		sets = append(sets, "dns_target = ?")
		args = append(args, binding.dnsTarget)
	}
	if req.NetworkMode != nil {
		sets = append(sets, "network_mode = ?")
		args = append(args, binding.networkMode)
	}
	if req.ProxyKind != nil {
		sets = append(sets, "proxy_kind = ?")
		args = append(args, binding.proxyKind)
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
	writeJSON(w, http.StatusOK, map[string]any{"domain": updated, "instructions": a.manualInstructions(updated)})
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Ownership proof: a TXT record at _texshare-verify.<hostname>.
	txts, txtErr := net.DefaultResolver.LookupTXT(ctx, "_texshare-verify."+d.Hostname)
	verified := false
	for _, t := range txts {
		if util.ConstantEqual(strings.TrimSpace(t), d.VerificationToken) {
			verified = true
		}
	}
	_, _ = a.db.Exec(`UPDATE domains SET verified = ? WHERE id = ?`, boolInt(verified), id)

	st := a.verifyDomain(ctx, d)
	a.storeDomainStatus(d.ID, st)
	if txtErr != nil && len(st.Errors) == 0 {
		st.Errors = append(st.Errors, "TXT-Lookup: "+txtErr.Error())
	}
	a.audit(actor.ID, actor.Username, "domain.verify", "domain", fmt.Sprint(id), "success",
		map[string]any{"hostname": d.Hostname, "verified": verified, "reachable": st.Reachable})
	updated, _ := a.domainByID(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": updated, "status": st,
		"dnsVerified": verified, "reachable": st.Reachable, "tlsStatus": updated.TLSStatus,
		"txtRecord": "_texshare-verify." + d.Hostname, "txtValue": d.VerificationToken,
		"instructions": a.manualInstructions(updated),
	})
}

func (a *App) adminCheckDomainHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	st := a.verifyDomain(ctx, d)
	a.storeDomainStatus(d.ID, st)
	updated, _ := a.domainByID(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": updated, "status": st, "reachable": st.Reachable, "tlsStatus": updated.TLSStatus,
		"error": strings.Join(st.Errors, "; "),
	})
}
