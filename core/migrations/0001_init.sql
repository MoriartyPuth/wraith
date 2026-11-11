-- WRAITH Core schema v1.
-- Timestamps are Unix milliseconds (INTEGER) in UTC.

CREATE TABLE sensors (
    id               TEXT PRIMARY KEY,
    kind             TEXT NOT NULL,              -- wraith-nano, tshark, kismet
    firmware_version TEXT,
    board            TEXT,
    state            TEXT NOT NULL DEFAULT 'unknown',
    capabilities     TEXT NOT NULL DEFAULT '[]', -- JSON array
    boot_id          TEXT,
    first_seen       INTEGER NOT NULL,
    last_seen        INTEGER NOT NULL,
    last_heartbeat   INTEGER,
    current_channel  INTEGER,
    rejected_count   INTEGER NOT NULL DEFAULT 0,
    duplicate_count  INTEGER NOT NULL DEFAULT 0,
    gap_count        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE access_points (
    bssid        TEXT PRIMARY KEY,
    ssid         TEXT NOT NULL DEFAULT '',
    ssid_hex     TEXT NOT NULL DEFAULT '',
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    last_channel INTEGER,
    last_rssi    INTEGER,
    auth_mode    TEXT,
    rsn          TEXT,                         -- JSON RSN object, NULL if none seen
    pmf          TEXT NOT NULL DEFAULT 'unavailable',
    last_sensor  TEXT
);
CREATE INDEX idx_ap_ssid ON access_points(ssid);
CREATE INDEX idx_ap_last_seen ON access_points(last_seen);

CREATE TABLE events (
    id           INTEGER PRIMARY KEY,
    event_id     TEXT NOT NULL,
    sensor_id    TEXT NOT NULL,
    source       TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    bssid        TEXT,
    channel      INTEGER,
    sensor_ts    INTEGER NOT NULL,             -- seconds as sent; 0 = unknown
    received_at  INTEGER NOT NULL,
    payload      TEXT NOT NULL,                -- original validated JSON
    UNIQUE (sensor_id, event_id)
);
CREATE INDEX idx_events_received ON events(received_at);
CREATE INDEX idx_events_type ON events(event_type, received_at);
CREATE INDEX idx_events_bssid ON events(bssid, received_at);

CREATE TABLE observations (
    id          INTEGER PRIMARY KEY,
    event_row   INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    bssid       TEXT NOT NULL,
    sensor_id   TEXT NOT NULL,
    channel     INTEGER,
    rssi        INTEGER,
    observed_at INTEGER NOT NULL
);
CREATE INDEX idx_obs_bssid ON observations(bssid, observed_at);

CREATE TABLE findings (
    id             TEXT PRIMARY KEY,
    category       TEXT NOT NULL,
    severity       TEXT NOT NULL CHECK (severity IN ('info','low','medium','high')),
    confidence     REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    classification TEXT NOT NULL CHECK (classification IN ('observed','suspicious','potential_attack','confirmed')),
    bssid          TEXT,
    sensor_id      TEXT,
    explanation    TEXT NOT NULL,
    evidence       TEXT NOT NULL DEFAULT '[]', -- JSON array of event_ids
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewed','dismissed')),
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_findings_created ON findings(created_at);
CREATE INDEX idx_findings_status ON findings(status, created_at);

CREATE TABLE capture_sessions (
    id         INTEGER PRIMARY KEY,
    provider   TEXT NOT NULL,
    interface  TEXT,
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
    status     TEXT NOT NULL,
    note       TEXT
);

CREATE TABLE app_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
