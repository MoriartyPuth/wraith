-- Phase 3: rogue-AP lookups by raw SSID, finding limitations.
CREATE INDEX idx_ap_ssid_hex ON access_points(ssid_hex);
ALTER TABLE findings ADD COLUMN limitations TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_obs_sensor ON observations(sensor_id, observed_at);
