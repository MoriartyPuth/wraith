-- Operator-assigned names for device addresses ("my iPhone"). Keyed by MAC
-- address, so a label follows that address across APs. User data: never
-- pruned by retention. Private (randomised) addresses can change, in which
-- case the old label simply stops matching.
CREATE TABLE device_labels (
    address    TEXT PRIMARY KEY,
    label      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
