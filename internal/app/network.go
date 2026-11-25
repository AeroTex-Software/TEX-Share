package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/dns/cloudflare"
	"github.com/AeroTex-Software/TEX-Share/internal/dns/ddns"
	"github.com/AeroTex-Software/TEX-Share/internal/dns/manual"
	"github.com/AeroTex-Software/TEX-Share/internal/dns/provider"
	"github.com/AeroTex-Software/TEX-Share/internal/network/proxy"
	"github.com/AeroTex-Software/TEX-Share/internal/network/verification"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

// networkManager owns the DNS provider registry and the DDNS engine. It is the
// single place that knows which DNS providers exist; the rest of the app talks
// to providers through the provider.Provider interface only.
type networkManager struct {
	app      *App
	registry *provider.Registry
	ddns     *ddns.Engine
}

func (a *App) initNetwork() {
	m := &networkManager{app: a, registry: provider.NewRegistry()}
	m.registry.Register("manual", func() provider.Provider { return manual.New() })
	m.registry.Register("none", func() provider.Provider { return manual.New() })
	m.registry.Register("cloudflare", func() provider.Provider { return cloudflare.New(a.cfAPIToken()) })
	m.ddns = ddns.New(a.settings, a.log)
	a.network = m
}

/* -------------------------------------------------------------------------- */
/* Trusted proxies                                                            */
/* -------------------------------------------------------------------------- */

// refreshTrustedProxies rebuilds the trusted-proxy set from configuration and
// the trusted_proxies setting. The legacy TEXSHARE_TRUST_PROXY boolean maps to
// loopback only, which is the safe default for a local reverse proxy/tunnel.
func (a *App) refreshTrustedProxies() {
	specs := append([]string{}, a.cfg.TrustedProxies...)
	if s := a.settings.Get("trusted_proxies"); strings.TrimSpace(s) != "" {
		specs = append(specs, s)
	}
	if len(specs) == 0 && a.cfg.TrustProxy {
		specs = append(specs, "127.0.0.1/32", "::1/128")
	}
	t := proxy.ParseTrusted(specs)
	a.trustMu.Lock()
	a.trustedProxies = t
	a.trustMu.Unlock()
}

func (a *App) trusted() *proxy.Trusted {
	a.trustMu.RLock()
	t := a.trustedProxies
	a.trustMu.RUnlock()
	if t == nil {
		return proxy.ParseTrusted(nil)
	}
	return t
}

// clientIP returns the real client address, honouring forwarded headers only
// from trusted proxies.
func (a *App) clientIP(r *http.Request) string { return proxy.ClientIP(r, a.trusted()) }

// isHTTPS reports whether the request reached the client over HTTPS, taking a
// trusted proxy's X-Forwarded-Proto into account.
func (a *App) isHTTPS(r *http.Request) bool { return proxy.IsHTTPS(r, a.trusted()) }

// forwardedHost returns the original host as seen by a trusted proxy.
func (a *App) forwardedHost(r *http.Request) string { return proxy.ForwardedHost(r, a.trusted()) }

/* -------------------------------------------------------------------------- */
/* Cloudflare DNS credentials                                                 */
/* -------------------------------------------------------------------------- */

const cfDNSSecretPurpose = "cloudflare_dns"

func (a *App) cfAPIToken() string {
	enc := strings.TrimSpace(a.settings.Get("cf_api_token_enc"))
	if enc == "" {
		return ""
	}
	raw, err := a.secrets.Open(cfDNSSecretPurpose, enc)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (a *App) setCFAPIToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return a.settings.Set("cf_api_token_enc", "")
	}
	enc, err := a.secrets.Seal(cfDNSSecretPurpose, []byte(token))
	if err != nil {
		return err
	}
	return a.settings.Set("cf_api_token_enc", enc)
}

/* -------------------------------------------------------------------------- */
/* Domain binding helpers                                                     */
/* -------------------------------------------------------------------------- */

func (a *App) globalDNSProvider() string {
	v := strings.ToLower(strings.TrimSpace(a.settings.Get("network_dns_provider")))
	if v == "" {
		return "manual"
	}
	return v
}

func (a *App) globalNetworkMode() string {
	v := strings.ToLower(strings.TrimSpace(a.settings.Get("network_connection")))
	if v == "" {
		return "direct"
	}
	return v
}

func (a *App) globalProxyKind() string {
	v := strings.ToLower(strings.TrimSpace(a.settings.Get("network_proxy_kind")))
	if v == "" {
		return "none"
	}
	return v
}

func (a *App) effDNSProvider(d *Domain) string {
	if strings.TrimSpace(d.DNSProvider) != "" {
		return strings.ToLower(strings.TrimSpace(d.DNSProvider))
	}
	return a.globalDNSProvider()
}

func (a *App) effNetworkMode(d *Domain) string {
	if strings.TrimSpace(d.NetworkMode) != "" {
		return strings.ToLower(strings.TrimSpace(d.NetworkMode))
	}
	return a.globalNetworkMode()
}

func (a *App) effProxyKind(d *Domain) string {
	if strings.TrimSpace(d.ProxyKind) != "" {
		return strings.ToLower(strings.TrimSpace(d.ProxyKind))
	}
	return a.globalProxyKind()
}

// buildDNSProvider resolves a provider by name.
func (a *App) buildDNSProvider(name string) (provider.Provider, error) {
	if a.network == nil {
		return nil, errors.New("network manager not initialised")
	}
	return a.network.registry.Build(name)
}

// providerSupportsAPI reports whether a provider can manage records via an API.
func providerSupportsAPI(name string) bool {
	switch strings.ToLower(name) {
	case "cloudflare":
		return true
	default:
		return false
	}
}

// tunnelCNAMETarget derives the Cloudflare tunnel CNAME target.
func (a *App) tunnelCNAMETarget() string {
	if a.cloudflare == nil {
		return ""
	}
	id := a.cloudflare.tunnelID()
	if id == "" {
		return ""
	}
	return id + ".cfargotunnel.com"
}

// suggestTarget derives a sensible DNS target for a domain from its network
// mode and the known public addresses.
func (a *App) suggestTarget(d *Domain) (rtype, content string, proxied bool) {
	mode := a.effNetworkMode(d)
	switch mode {
	case "tunnel":
		return provider.TypeCNAME, a.tunnelCNAMETarget(), true
	case "proxy":
		if d.DNSTarget != "" {
			return d.DNSRecordType, d.DNSTarget, false
		}
		if ip := a.settings.Get("public_ipv4"); ip != "" {
			return provider.TypeA, ip, false
		}
		return provider.TypeCNAME, "<proxy-hostname>", false
	default: // direct, static, ddns
		if d.DNSRecordType == provider.TypeAAAA {
			return provider.TypeAAAA, a.settings.Get("public_ipv6"), false
		}
		return provider.TypeA, a.settings.Get("public_ipv4"), false
	}
}

// manualInstructions builds the DNS instruction table for a domain.
func (a *App) manualInstructions(d *Domain) []manual.Instruction {
	rtype := d.DNSRecordType
	if rtype == "" {
		rtype, _, _ = a.suggestTarget(d)
	}
	target := ""
	if a.effNetworkMode(d) == "tunnel" {
		target = a.tunnelCNAMETarget()
	}
	out := manual.Instructions(d.Hostname, a.effNetworkMode(d), rtype,
		a.settings.Get("public_ipv4"), a.settings.Get("public_ipv6"), target)
	// An explicitly configured target wins over the derived default.
	if t := strings.TrimSpace(d.DNSTarget); t != "" {
		for i := range out {
			switch out[i].Type {
			case provider.TypeA, provider.TypeAAAA, provider.TypeCNAME:
				out[i].Content = t
			}
		}
	}
	return out
}

// ensureZone finds the zone for a hostname, caching it on the domain row.
func (a *App) ensureZone(ctx context.Context, p provider.Provider, d *Domain) (provider.Zone, error) {
	if d.DNSZoneID != "" {
		return provider.Zone{ID: d.DNSZoneID}, nil
	}
	zones, err := p.Zones(ctx)
	if err != nil {
		return provider.Zone{}, err
	}
	zone, ok := provider.MatchZone(zones, d.Hostname)
	if !ok {
		return provider.Zone{}, errors.New("keine passende DNS-Zone gefunden")
	}
	return zone, nil
}

/* -------------------------------------------------------------------------- */
/* DDNS apply                                                                 */
/* -------------------------------------------------------------------------- */

// applyDDNS updates A/AAAA records for every enabled domain that uses an
// API-capable provider and a dynamic network mode.
func (a *App) applyDDNS(ctx context.Context, v4, v6 string) error {
	rows, err := a.db.Query(`SELECT id FROM domains WHERE enabled = 1`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	var firstErr error
	updated := 0
	for _, id := range ids {
		d, err := a.domainByID(id)
		if err != nil {
			continue
		}
		mode := a.effNetworkMode(d)
		if mode != "ddns" && mode != "direct" && mode != "static" {
			continue
		}
		name := a.effDNSProvider(d)
		if !providerSupportsAPI(name) {
			continue
		}
		p, err := a.buildDNSProvider(name)
		if err != nil {
			continue
		}
		zone, err := a.ensureZone(ctx, p, d)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if zone.ID != d.DNSZoneID {
			_, _ = a.db.Exec(`UPDATE domains SET dns_zone_id = ? WHERE id = ?`, zone.ID, d.ID)
		}
		if v4 != "" && d.DNSRecordType != provider.TypeAAAA {
			if _, _, err := provider.Upsert(ctx, p, zone.ID, provider.Record{Type: provider.TypeA, Name: d.Hostname, Content: v4}); err != nil {
				a.logDDNS(d, provider.TypeA, v4, "error", err.Error())
				if firstErr == nil {
					firstErr = err
				}
			} else {
				a.logDDNS(d, provider.TypeA, v4, "ok", "")
				updated++
			}
		}
		if v6 != "" && a.settings.Bool("ddns_ipv6") {
			if _, _, err := provider.Upsert(ctx, p, zone.ID, provider.Record{Type: provider.TypeAAAA, Name: d.Hostname, Content: v6}); err != nil {
				a.logDDNS(d, provider.TypeAAAA, v6, "error", err.Error())
				if firstErr == nil {
					firstErr = err
				}
			} else {
				a.logDDNS(d, provider.TypeAAAA, v6, "ok", "")
				updated++
			}
		}
	}
	if updated > 0 {
		a.audit(0, "system", "ddns.update", "network", "", "success", map[string]any{"updated": updated, "ipv4": v4, "ipv6": v6})
	}
	return firstErr
}

func (a *App) logDDNS(d *Domain, rtype, value, result, msg string) {
	old := d.DNSTarget
	_, _ = a.db.Exec(`INSERT INTO ddns_log(ts, provider, record_type, hostname, old_value, new_value, result, message)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, util.Now(), a.effDNSProvider(d), rtype, d.Hostname, old, value, result, msg)
	if result == "ok" {
		_, _ = a.db.Exec(`UPDATE domains SET dns_target = ?, dns_status = 'ok', dns_checked_at = ?, dns_ok = 1, dns_error = NULL WHERE id = ?`,
			value, util.Now(), d.ID)
	}
}

/* -------------------------------------------------------------------------- */
/* HTTP handlers                                                              */
/* -------------------------------------------------------------------------- */

// networkStatusHandler returns the whole network configuration and state.
func (a *App) networkStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.networkSnapshot())
}

func (a *App) networkSnapshot() map[string]any {
	cfg := map[string]string{}
	for _, k := range []string{
		"network_dns_provider", "network_connection", "network_proxy_kind", "network_proxy_hostname",
		"network_proxy_upstream", "network_admin_exposed", "trusted_proxies",
		"cf_account_id", "cf_zone_id", "cf_zone_name",
		"ddns_enabled", "ddns_provider", "ddns_interval_minutes", "ddns_ipv4", "ddns_ipv6",
		"public_ipv4", "public_ipv6",
	} {
		cfg[k] = a.settings.Get(k)
	}
	trusted := a.trusted().Strings()
	guide := proxy.BuildGuide(a.globalProxyKind(), proxy.Params{
		Hostname:      a.settings.Get("network_proxy_hostname"),
		AdminHostname: a.settings.Get("cf_hostname"),
		UpstreamHost:  a.settings.Get("network_proxy_upstream"),
		UpstreamPort:  publicPort(a.cfg.PublicAddr),
		TrustedCIDR:   strings.Join(trusted, ","),
	})
	out := map[string]any{
		"config":         cfg,
		"providers":      a.network.registry.Names(),
		"proxyKinds":     proxy.Kinds,
		"trustedProxies": trusted,
		"tokenSet":       a.cfAPIToken() != "",
		"guide":          guide,
		"publicPort":     publicPort(a.cfg.PublicAddr),
		"adminPort":      publicPort(a.cfg.AdminAddr),
	}
	if a.network != nil && a.network.ddns != nil {
		out["ddns"] = a.network.ddns.Status()
	}
	if a.cloudflare != nil {
		out["cloudflare"] = a.cloudflare.Status()
	}
	if a.tunnel != nil {
		out["tunnel"] = a.tunnel.Status()
	}
	return out
}

// networkPatchHandler validates and stores a subset of network settings.
func (a *App) networkPatchHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings map[string]string `json:"settings"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	clean := map[string]string{}
	for k, v := range req.Settings {
		valid, err := a.validateSetting(k, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_setting", err.Error())
			return
		}
		clean[k] = valid
	}
	if err := a.settings.SetMany(clean); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	a.refreshTrustedProxies()
	a.refreshHostCache()
	a.auditRequest(r, "network.update", "network", "", "success", map[string]any{"keys": sortedKeys(clean)})
	writeJSON(w, http.StatusOK, a.networkSnapshot())
}

// cloudflareTokenHandler stores (optionally) and tests a Cloudflare API token,
// then returns the visible zones.
func (a *App) cloudflareTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	if strings.TrimSpace(req.Token) != "" {
		if err := a.setCFAPIToken(req.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	}
	token := a.cfAPIToken()
	if token == "" {
		writeError(w, http.StatusBadRequest, "cloudflare_not_configured", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	client := cloudflare.New(token)
	if err := client.Test(ctx); err != nil {
		writeError(w, http.StatusBadRequest, "cloudflare_error", err.Error())
		return
	}
	zones, err := client.Zones(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cloudflare_error", err.Error())
		return
	}
	a.auditRequest(r, "cloudflare.token", "network", "", "success", map[string]any{"zones": len(zones)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "zones": zones})
}

// cloudflareZonesHandler lists zones for the stored token.
func (a *App) cloudflareZonesHandler(w http.ResponseWriter, r *http.Request) {
	token := a.cfAPIToken()
	if token == "" {
		writeError(w, http.StatusBadRequest, "cloudflare_not_configured", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	zones, err := cloudflare.New(token).Zones(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cloudflare_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"zones": zones})
}

// dnsApplyHandler creates or updates the DNS record for a domain using its
// configured provider. For manual providers it refuses to fake success.
func (a *App) dnsApplyHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID int64  `json:"domainId"`
		Type     string `json:"type"`
		Content  string `json:"content"`
		ZoneID   string `json:"zoneId"`
		Proxied  bool   `json:"proxied"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	d, err := a.domainByID(req.DomainID)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	name := a.effDNSProvider(d)
	if !providerSupportsAPI(name) {
		writeError(w, http.StatusBadRequest, "dns_manual", "Dieser DNS-Anbieter hat keine API – bitte die Records manuell anlegen.")
		return
	}
	p, err := a.buildDNSProvider(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	rtype := strings.ToUpper(strings.TrimSpace(req.Type))
	if rtype == "" {
		rtype = d.DNSRecordType
	}
	if rtype == "" {
		rtype = provider.TypeA
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		_, content, _ = a.suggestTarget(d)
	}
	if content == "" || strings.HasPrefix(content, "<") {
		writeError(w, http.StatusBadRequest, "dns_target_missing", "Kein gültiges DNS-Ziel bekannt.")
		return
	}
	ctx, cancel := contextWithTimeout(r, 30*time.Second)
	defer cancel()
	zone := provider.Zone{ID: req.ZoneID}
	if zone.ID == "" {
		z, zerr := a.ensureZone(ctx, p, d)
		if zerr != nil {
			writeError(w, http.StatusBadRequest, "dns_error", zerr.Error())
			return
		}
		zone = z
	}
	rec, created, err := provider.Upsert(ctx, p, zone.ID, provider.Record{Type: rtype, Name: d.Hostname, Content: content, Proxied: req.Proxied})
	if err != nil {
		_, _ = a.db.Exec(`UPDATE domains SET dns_status='error', dns_checked_at=?, dns_ok=0, dns_error=? WHERE id=?`, util.Now(), err.Error(), d.ID)
		writeError(w, http.StatusBadRequest, "dns_error", err.Error())
		return
	}
	_, _ = a.db.Exec(`UPDATE domains SET dns_provider=?, dns_record_type=?, dns_target=?, dns_zone_id=?, dns_record_id=?, dns_status='ok', dns_checked_at=?, dns_ok=1, dns_error=NULL WHERE id=?`,
		name, rtype, content, zone.ID, rec.ID, util.Now(), d.ID)
	a.auditRequest(r, "dns.apply", "domain", d.Hostname, "success", map[string]any{"type": rtype, "content": content, "created": created})
	updated, _ := a.domainByID(d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "created": created, "record": rec, "domain": updated})
}

// dnsDeleteHandler removes the managed DNS record for a domain.
func (a *App) dnsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID int64 `json:"domainId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	d, err := a.domainByID(req.DomainID)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	if d.DNSRecordID == "" || d.DNSZoneID == "" {
		writeError(w, http.StatusBadRequest, "dns_no_record", "")
		return
	}
	p, err := a.buildDNSProvider(a.effDNSProvider(d))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	if err := p.DeleteRecord(ctx, d.DNSZoneID, d.DNSRecordID); err != nil {
		writeError(w, http.StatusBadRequest, "dns_error", err.Error())
		return
	}
	_, _ = a.db.Exec(`UPDATE domains SET dns_record_id='', dns_status='unknown', dns_checked_at=?, dns_ok=NULL, dns_error=NULL WHERE id=?`, util.Now(), d.ID)
	a.auditRequest(r, "dns.delete", "domain", d.Hostname, "success", nil)
	updated, _ := a.domainByID(d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "domain": updated})
}

// networkDNSInstructionsHandler returns the manual DNS record table for a
// hostname and network mode. It performs no network access and creates nothing.
func (a *App) networkDNSInstructionsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	host := strings.TrimSpace(q.Get("hostname"))
	if host == "" {
		host = a.settings.Get("network_proxy_hostname")
	}
	mode := strings.ToLower(strings.TrimSpace(q.Get("mode")))
	if mode == "" {
		mode = a.globalNetworkMode()
	}
	rtype := strings.ToUpper(strings.TrimSpace(q.Get("type")))
	if rtype == "" {
		rtype = provider.TypeA
	}
	tunnelTarget := ""
	if mode == "tunnel" {
		tunnelTarget = a.tunnelCNAMETarget()
	}
	out := manual.Instructions(host, mode, rtype, a.settings.Get("public_ipv4"), a.settings.Get("public_ipv6"), tunnelTarget)
	writeJSON(w, http.StatusOK, map[string]any{"instructions": out})
}

// networkGuideHandler renders a reverse-proxy guide for a chosen kind.
func (a *App) networkGuideHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := strings.ToLower(strings.TrimSpace(q.Get("kind")))
	if !proxy.ValidKind(kind) {
		kind = a.globalProxyKind()
	}
	host := strings.TrimSpace(q.Get("hostname"))
	if host == "" {
		host = a.settings.Get("network_proxy_hostname")
	}
	guide := proxy.BuildGuide(kind, proxy.Params{
		Hostname:      host,
		AdminHostname: a.settings.Get("cf_hostname"),
		UpstreamHost:  a.settings.Get("network_proxy_upstream"),
		UpstreamPort:  publicPort(a.cfg.PublicAddr),
		TrustedCIDR:   strings.Join(a.trusted().Strings(), ","),
	})
	writeJSON(w, http.StatusOK, map[string]any{"guide": guide})
}

// networkDetectIPHandler detects the public addresses on demand.
func (a *App) networkDetectIPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 15*time.Second)
	defer cancel()
	v4, err4 := ddns.DetectPublicIPv4(ctx)
	v6, err6 := ddns.DetectPublicIPv6(ctx)
	if v4 != "" {
		_ = a.settings.Set("public_ipv4", v4)
	}
	if v6 != "" {
		_ = a.settings.Set("public_ipv6", v6)
	}
	out := map[string]any{"ipv4": v4, "ipv6": v6}
	if err4 != nil {
		out["ipv4Error"] = err4.Error()
	}
	if err6 != nil {
		out["ipv6Error"] = err6.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// networkDDNSRunHandler triggers a DDNS cycle immediately.
func (a *App) networkDDNSRunHandler(w http.ResponseWriter, r *http.Request) {
	if a.network == nil || a.network.ddns == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 40*time.Second)
	defer cancel()
	cfg := a.ddnsConfig()
	err := a.network.ddns.RunOnce(ctx, a.applyDDNS, cfg)
	a.auditRequest(r, "ddns.run", "network", "", "success", nil)
	out := map[string]any{"ok": err == nil, "ddns": a.network.ddns.Status()}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) ddnsConfig() ddns.Config {
	return ddns.Config{
		Enabled:  a.settings.Bool("ddns_enabled"),
		Interval: time.Duration(a.settings.Int("ddns_interval_minutes")) * time.Minute,
		IPv4:     a.settings.Bool("ddns_ipv4"),
		IPv6:     a.settings.Bool("ddns_ipv6"),
	}
}

// networkDDNSLogHandler returns recent DDNS log entries.
func (a *App) networkDDNSLogHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, ts, provider, record_type, hostname, old_value, new_value, result, message
		FROM ddns_log ORDER BY id DESC LIMIT 50`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, ts int64
		var prov, rtype, host, oldV, newV, result, msg string
		if rows.Scan(&id, &ts, &prov, &rtype, &host, &oldV, &newV, &result, &msg) == nil {
			out = append(out, map[string]any{
				"id": id, "ts": ts, "provider": prov, "recordType": rtype, "hostname": host,
				"oldValue": oldV, "newValue": newV, "result": result, "message": msg,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// networkVerifyHandler runs a full verification for one domain and caches it.
func (a *App) networkVerifyHandler(w http.ResponseWriter, r *http.Request) {
	id := util.ParseInt(r.PathValue("id"), 0)
	d, err := a.domainByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "domain_not_found", "")
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	st := a.verifyDomain(ctx, d)
	a.storeDomainStatus(d.ID, st)
	updated, _ := a.domainByID(d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"domain": updated, "status": st})
}

// verifyDomain runs the verification package with the domain's bindings.
func (a *App) verifyDomain(ctx context.Context, d *Domain) verification.Status {
	expectIPs := []string{}
	if d.DNSTarget != "" {
		expectIPs = append(expectIPs, d.DNSTarget)
	}
	mode := a.effNetworkMode(d)
	allowPrivate := mode == "direct" || mode == "static" || mode == "proxy"
	adminHost := ""
	if a.settings.Bool("network_admin_exposed") {
		adminHost = a.settings.Get("network_proxy_hostname")
	}
	return verification.Check(ctx, verification.Options{
		Hostname:      d.Hostname,
		AdminHostname: adminHost,
		ExpectIPs:     expectIPs,
		AllowPrivate:  allowPrivate,
	})
}

// storeDomainStatus caches a verification result.
func (a *App) storeDomainStatus(domainID int64, st verification.Status) {
	raw := util.JSON(st)
	_, _ = a.db.Exec(`INSERT INTO domain_status(domain_id, status_json, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(domain_id) DO UPDATE SET status_json=excluded.status_json, updated_at=excluded.updated_at`,
		domainID, raw, util.Now())
	tlsStatus := "missing"
	if st.CertValid {
		tlsStatus = "valid"
	} else if st.HTTPS {
		tlsStatus = "invalid"
	}
	ok := st.DNSResolved && st.Reachable
	errText := ""
	if len(st.Errors) > 0 {
		errText = strings.Join(st.Errors, "; ")
	}
	_, _ = a.db.Exec(`UPDATE domains SET tls_status=?, last_checked_at=?, last_check_ok=?, last_check_error=? WHERE id=?`,
		tlsStatus, util.Now(), boolInt(ok), nullString(errText), domainID)
}

// publicPort extracts the port from a listen address.
func publicPort(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return "3535"
}
