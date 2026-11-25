-- One-time tickets for client-built E2EE ZIP downloads. A ticket reserves one
-- download and is consumed atomically so it cannot be replayed.
CREATE TABLE zip_tickets (
    nonce      TEXT PRIMARY KEY,
    share_id   TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_zip_tickets_expiry ON zip_tickets(expires_at);
