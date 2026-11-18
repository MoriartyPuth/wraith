-- Bluetooth watchlist (operator-selected addresses) with derived presence.
-- Presence sessions are computed from bt_observations; this table only keeps
-- the operator's selection and the last evaluated state so arrival/departure
-- findings survive Core restarts. Never pruned (user data).
CREATE TABLE bt_watchlist (
    transport    TEXT    NOT NULL,
    address      TEXT    NOT NULL,
    address_type TEXT    NOT NULL DEFAULT '',
    label        TEXT    NOT NULL DEFAULT '',
    added_at     INTEGER NOT NULL,
    present      INTEGER NOT NULL DEFAULT 0, -- last evaluated: 0 away, 1 present
    state_since  INTEGER,                    -- when `present` last changed
    last_eval_at INTEGER,                    -- last presence evaluation
    PRIMARY KEY (transport, address, address_type)
);
CREATE INDEX idx_bt_watchlist_address ON bt_watchlist(address);
