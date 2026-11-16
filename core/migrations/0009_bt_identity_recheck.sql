-- Re-examine BLE rows that 0008's first backfill marked "nothing recognised"
-- (ident_kind = ''): the backfill now also falls back to the row's stored
-- manufacturer id and service UUIDs when the row has no stored adverts.
UPDATE bt_devices SET identity = NULL, ident_kind = NULL WHERE ident_kind = '';
