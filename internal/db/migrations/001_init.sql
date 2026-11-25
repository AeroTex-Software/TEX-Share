-- TEX Share initial schema (v1)
PRAGMA foreign_keys = ON;

CREATE TABLE users (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT NOT NULL COLLATE NOCASE UNIQUE,
    display_name          TEXT NOT NULL,
    password_hash         TEXT NOT NULL,
    role                  TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
    must_change_password  INTEGER NOT NULL DEFAULT 0,
    is_suspended          INTEGER NOT NULL DEFAULT 0,
    suspended_at          INTEGER,
    suspended_reason      TEXT,
    namespace             TEXT COLLATE NOCASE UNIQUE,
    storage_limit_bytes   INTEGER NOT NULL DEFAULT 0,
    storage_used_bytes    INTEGER NOT NULL DEFAULT 0,
    storage_reserved_bytes INTEGER NOT NULL DEFAULT 0,
    avatar_path           TEXT,
    totp_secret           TEXT,
    totp_enabled          INTEGER NOT NULL DEFAULT 0,
    totp_last_step        INTEGER NOT NULL DEFAULT 0,
    language              TEXT NOT NULL DEFAULT 'auto',
    theme                 TEXT NOT NULL DEFAULT 'system',
    notification_prefs    TEXT NOT NULL DEFAULT '{}',
    failed_login_count    INTEGER NOT NULL DEFAULT 0,
    locked_until          INTEGER,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL,
    last_login_at         INTEGER
);

CREATE TABLE domains (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    hostname           TEXT NOT NULL COLLATE NOCASE UNIQUE,
    enabled            INTEGER NOT NULL DEFAULT 1,
    verified           INTEGER NOT NULL DEFAULT 0,
    verification_token TEXT NOT NULL,
    is_default         INTEGER NOT NULL DEFAULT 0,
    tls_status         TEXT NOT NULL DEFAULT 'unknown',
    last_checked_at    INTEGER,
    last_check_ok      INTEGER,
    last_check_error   TEXT,
    created_at         INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_domains_default ON domains(is_default) WHERE is_default = 1;

CREATE TABLE user_domains (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, domain_id)
);

CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL UNIQUE,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    realm        TEXT NOT NULL CHECK (realm IN ('public','admin')),
    csrf_token   TEXT NOT NULL,
    remember     INTEGER NOT NULL DEFAULT 0,
    user_agent   TEXT NOT NULL DEFAULT '',
    browser      TEXT NOT NULL DEFAULT '',
    os           TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id, realm);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE login_challenges (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    realm      TEXT NOT NULL,
    remember   INTEGER NOT NULL DEFAULT 0,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE recovery_codes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_recovery_user ON recovery_codes(user_id);

CREATE TABLE shares (
    id                 TEXT PRIMARY KEY,
    owner_id           INTEGER REFERENCES users(id) ON DELETE SET NULL,
    owner_username     TEXT NOT NULL,
    type               TEXT NOT NULL CHECK (type IN ('file','link','text','media','folder')),
    domain_id          INTEGER NOT NULL REFERENCES domains(id),
    namespace          TEXT NOT NULL DEFAULT '',
    slug               TEXT NOT NULL,
    token_hash         TEXT NOT NULL UNIQUE,
    title              TEXT,
    description        TEXT,
    link_url           TEXT,
    text_content       TEXT,
    password_hash      TEXT,
    is_e2ee            INTEGER NOT NULL DEFAULT 0,
    e2ee_mode          TEXT CHECK (e2ee_mode IS NULL OR e2ee_mode IN ('key','password')),
    e2ee_manifest      BLOB,
    e2ee_key_wrap      TEXT,
    e2ee_recovery_wrap TEXT,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','expired','deleted')),
    admin_locked       INTEGER NOT NULL DEFAULT 0,
    admin_lock_reason  TEXT,
    suspended_block    INTEGER NOT NULL DEFAULT 0,
    expires_at         INTEGER,
    download_limit     INTEGER NOT NULL DEFAULT 0,
    download_count     INTEGER NOT NULL DEFAULT 0,
    view_count         INTEGER NOT NULL DEFAULT 0,
    bytes_served       INTEGER NOT NULL DEFAULT 0,
    size_bytes         INTEGER NOT NULL DEFAULT 0,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    deleted_at         INTEGER,
    purge_after        INTEGER,
    last_accessed_at   INTEGER,
    version            INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX idx_shares_slug ON shares(domain_id, namespace, slug);
CREATE INDEX idx_shares_owner ON shares(owner_id, status);
CREATE INDEX idx_shares_status ON shares(status);
CREATE INDEX idx_shares_expiry ON shares(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_shares_purge ON shares(purge_after) WHERE purge_after IS NOT NULL;

CREATE TABLE share_files (
    id                  TEXT PRIMARY KEY,
    share_id            TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    position            INTEGER NOT NULL DEFAULT 0,
    name                TEXT NOT NULL DEFAULT '',
    mime                TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes          INTEGER NOT NULL,
    encrypted_size_bytes INTEGER NOT NULL DEFAULT 0,
    storage_path        TEXT NOT NULL,
    sha256              TEXT NOT NULL DEFAULT '',
    scan_status         TEXT NOT NULL DEFAULT 'skipped',
    scan_message        TEXT,
    created_at          INTEGER NOT NULL
);
CREATE INDEX idx_files_share ON share_files(share_id, position);

CREATE TABLE uploads (
    id           TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    mime         TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL,
    chunk_size   INTEGER NOT NULL DEFAULT 0,
    total_chunks INTEGER NOT NULL DEFAULT 0,
    encrypted    INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','uploading','complete','attached','cancelled')),
    staging_dir  TEXT NOT NULL,
    storage_path TEXT,
    sha256       TEXT NOT NULL DEFAULT '',
    scan_status  TEXT NOT NULL DEFAULT 'skipped',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX idx_uploads_user ON uploads(user_id, status);
CREATE INDEX idx_uploads_expiry ON uploads(expires_at) WHERE status IN ('pending','uploading','complete');

CREATE TABLE share_access (
    token_hash TEXT PRIMARY KEY,
    share_id   TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_share_access_expiry ON share_access(expires_at);

CREATE TABLE notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    data       TEXT NOT NULL DEFAULT '{}',
    read_at    INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_notifications_user ON notifications(user_id, created_at DESC);

CREATE TABLE activity (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    share_id   TEXT,
    type       TEXT NOT NULL,
    meta       TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_activity_user ON activity(user_id, created_at DESC);

CREATE TABLE stats_daily (
    share_id  TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    day       TEXT NOT NULL,
    views     INTEGER NOT NULL DEFAULT 0,
    downloads INTEGER NOT NULL DEFAULT 0,
    bytes     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (share_id, day)
);

CREATE TABLE audit_log (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             INTEGER NOT NULL,
    actor_id       INTEGER,
    actor_username TEXT NOT NULL,
    action         TEXT NOT NULL,
    object_type    TEXT NOT NULL DEFAULT '',
    object_id      TEXT NOT NULL DEFAULT '',
    result         TEXT NOT NULL DEFAULT 'ok',
    details        TEXT NOT NULL DEFAULT '{}',
    prev_hash      TEXT NOT NULL,
    entry_hash     TEXT NOT NULL
);
CREATE INDEX idx_audit_ts ON audit_log(ts DESC);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE backups (
    id           TEXT PRIMARY KEY,
    filename     TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    kind         TEXT NOT NULL DEFAULT 'manual',
    encrypted    INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'complete',
    error        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_backups_created ON backups(created_at DESC);

CREATE TABLE password_resets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id   INTEGER,
    created_at INTEGER NOT NULL
);
