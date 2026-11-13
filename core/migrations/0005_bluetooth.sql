-- Bluetooth BT-2: device inventory for protocol v4 bluetooth.* events
-- (docs/bluetooth/event-protocol.md). Additive only: no existing table is
-- altered. Wi-Fi tables are untouched.

-- One row per observed Bluetooth address. An address is an observation, not
-- a physical device: BLE random addresses rotate and are never merged.
-- address_type is '' for Classic (BD_ADDR is public) and 'public' | 'random'
-- for BLE, so it can be part of the primary key without NULL ambiguity.
CREATE TABLE bt_devices (
    transport       TEXT    NOT NULL CHECK (transport IN ('classic', 'ble')),
    address         TEXT    NOT NULL,
    address_type    TEXT    NOT NULL DEFAULT '',
    name            TEXT,
    cod             INTEGER,          -- Classic: raw 24-bit Class of Device (observed)
    device_class    TEXT,             -- Classic: decoded from CoD
    classification  TEXT,             -- BLE: inferred by the sensor
    confidence      REAL,             -- BLE: confidence of the inference
    manufacturer_id INTEGER,
    tx_power        INTEGER,
    services        TEXT    NOT NULL DEFAULT '[]', -- JSON array, union of advertised UUIDs
    last_rssi       INTEGER,
    rssi_avg        REAL,             -- exponentially weighted moving average
    observations    INTEGER NOT NULL DEFAULT 0,
    first_seen      INTEGER NOT NULL,
    last_seen       INTEGER NOT NULL,
    last_sensor     TEXT,
    PRIMARY KEY (transport, address, address_type)
);
CREATE INDEX idx_bt_devices_last_seen ON bt_devices(last_seen);
CREATE INDEX idx_bt_devices_transport ON bt_devices(transport, last_seen);
CREATE INDEX idx_bt_devices_address ON bt_devices(address);

-- One row per stored Bluetooth event that names a device: RSSI history and
-- the link from a device to its events. Removed with the event by retention
-- pruning (ON DELETE CASCADE); idx_bt_obs_event_row keeps that cascade from
-- scanning the table (see migration 0003 for the Wi-Fi equivalent).
CREATE TABLE bt_observations (
    event_row    INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    transport    TEXT    NOT NULL,
    address      TEXT    NOT NULL,
    address_type TEXT    NOT NULL DEFAULT '',
    event_type   TEXT    NOT NULL,
    rssi         INTEGER,
    sensor_id    TEXT    NOT NULL,
    observed_at  INTEGER NOT NULL
);
CREATE INDEX idx_bt_obs_event_row ON bt_observations(event_row);
CREATE INDEX idx_bt_obs_device ON bt_observations(transport, address, address_type, observed_at);

-- Latest Bluetooth subsystem state per sensor (bluetooth.system.status /
-- bluetooth.system.error).
CREATE TABLE bt_sensor_status (
    sensor_id          TEXT PRIMARY KEY,
    mode               TEXT,
    state              TEXT,
    classic_devices    INTEGER,
    ble_devices        INTEGER,
    message            TEXT,
    updated_at         INTEGER,
    error_count        INTEGER NOT NULL DEFAULT 0,
    last_error_code    TEXT,
    last_error_message TEXT,
    last_error_at      INTEGER
);
