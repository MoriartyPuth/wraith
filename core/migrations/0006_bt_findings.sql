-- Bluetooth BT-10: findings about a Bluetooth device name it here, as the
-- bt_devices row id (storage.BTDeviceID: "classic:AA:..", "ble:random:AA:..").
-- Additive: existing findings keep NULL; Wi-Fi findings never set it.
ALTER TABLE findings ADD COLUMN bt_device TEXT;
CREATE INDEX idx_findings_dedup ON findings(category, created_at);
