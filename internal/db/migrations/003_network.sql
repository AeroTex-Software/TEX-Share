-- Flexible domain / DNS / network management (v3)
--
-- The three concerns are deliberately decoupled:
--   * DNS management   -> domains.dns_provider + provider adapters
--   * network path     -> domains.network_mode / proxy_kind
--   * reverse proxy    -> proxy_kind + trusted_proxies setting
--
-- Cloudflare is only one of several DNS providers and is never required.

-- Per-domain DNS / network binding. Empty values mean "inherit the global
-- setting" so existing domains keep working unchanged after the upgrade.
ALTER TABLE domains ADD COLUMN dns_provider     TEXT    NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN dns_record_type  TEXT    NOT NULL DEFAULT 'A';
ALTER TABLE domains ADD COLUMN dns_target       TEXT    NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN dns_zone_id      TEXT    NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN dns_record_id    TEXT    NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN dns_status       TEXT    NOT NULL DEFAULT 'unknown';
ALTER TABLE domains ADD COLUMN dns_checked_at   INTEGER;
ALTER TABLE domains ADD COLUMN dns_ok           INTEGER;
ALTER TABLE domains ADD COLUMN dns_error        TEXT;
ALTER TABLE domains ADD COLUMN network_mode     TEXT    NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN proxy_kind       TEXT    NOT NULL DEFAULT '';

-- Cached, per-domain verification result (JSON) so the admin UI can render the
-- full status matrix without re-running probes on every page load.
CREATE TABLE domain_status (
    domain_id   INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
    status_json TEXT NOT NULL DEFAULT '{}',
    updated_at  INTEGER NOT NULL DEFAULT 0
);

-- Audit trail for the DDNS engine. Credentials are never written here.
CREATE TABLE ddns_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    provider    TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    hostname    TEXT NOT NULL DEFAULT '',
    old_value   TEXT NOT NULL DEFAULT '',
    new_value   TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL DEFAULT 'ok',
    message     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_ddns_log_ts ON ddns_log(ts DESC);
