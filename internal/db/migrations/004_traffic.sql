-- Daily network traffic counters (v4)
--
-- The request middleware keeps monotonic in-memory byte counters (tx = bytes
-- written to public clients, rx = bytes read from public clients). A background
-- job folds their deltas into this table once a minute so the dashboard can
-- chart download/upload traffic over time. Counters are process-local and reset
-- on restart; the flush job detects a decrease and treats it as a restart.

CREATE TABLE IF NOT EXISTS traffic_daily (
    day      TEXT PRIMARY KEY,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_bytes INTEGER NOT NULL DEFAULT 0
);
