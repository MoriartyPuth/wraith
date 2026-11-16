-- Bluetooth device identity decoded by Core from what the device advertises
-- (internal/btident: Apple Continuity messages, tracker service UUIDs).
-- identity is the JSON of btident.Identity (kind, label, source, rank,
-- tracker, separated, battery, at); ident_kind duplicates its kind for
-- filtering. NULL = not decoded yet; '' kind = checked, nothing recognised.
ALTER TABLE bt_devices ADD COLUMN identity TEXT;
ALTER TABLE bt_devices ADD COLUMN ident_kind TEXT;
CREATE INDEX idx_bt_devices_kind ON bt_devices(ident_kind, last_seen);
