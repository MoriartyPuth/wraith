-- Phase 8: client (station) sightings from 802.11 data frames (protocol v3).
-- A row means "this station was seen exchanging data with this BSSID"; it is
-- not authoritative association state (idle clients are invisible and MACs
-- may be randomised).
CREATE TABLE clients (
    bssid        TEXT NOT NULL,
    client       TEXT NOT NULL,
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    frames       INTEGER NOT NULL DEFAULT 0,
    last_rssi    INTEGER,
    last_channel INTEGER,
    last_sensor  TEXT,
    PRIMARY KEY (bssid, client)
);
CREATE INDEX idx_clients_last_seen ON clients(last_seen);
CREATE INDEX idx_clients_bssid ON clients(bssid, last_seen);
CREATE INDEX idx_clients_client ON clients(client, last_seen);
