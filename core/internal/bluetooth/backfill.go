package bluetooth

import (
	"context"
	"encoding/json"

	"github.com/MoriartyPuth/wraith/core/internal/btident"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// backfillAdverts bounds how many stored adverts are decoded per device.
const backfillAdverts = 200

// BackfillIdentities decodes an identity for BLE devices stored before Core
// decoded identities (migration 0008), from their stored advertisement
// events. Each row is examined once: afterwards it is marked checked even if
// nothing was recognised. It returns how many rows it examined and how many
// got an identity. Safe to run while ingesting: it only fills rows that are
// still unchecked, and new events decode on arrival.
func BackfillIdentities(ctx context.Context, s *storage.Store) (checked, identified int, err error) {
	for {
		keys, err := s.UncheckedBLEDevices(ctx, 256)
		if err != nil || len(keys) == 0 {
			return checked, identified, err
		}
		for _, k := range keys {
			metas, times, err := s.RecentBLEAdverts(ctx, k[1], k[2], backfillAdverts)
			if err != nil {
				return checked, identified, err
			}
			var best *btident.Identity
			for i := len(metas) - 1; i >= 0; i-- { // oldest first, so equal ranks keep the newest
				var m BLEAdvMeta
				if json.Unmarshal(metas[i], &m) != nil {
					continue
				}
				if id := decode(m.ManufacturerID, m.ManufacturerData, m.Services, times[i]); btident.Better(id, best) {
					best = id
				}
			}
			if best == nil { // no stored adverts (device records only): use the row's own fields
				if d, err := s.GetBTDevice(ctx, k[0], k[1], k[2]); err == nil {
					best = decode(d.ManufacturerID, nil, d.Services, d.LastSeen)
				}
			}
			if err := s.SetBTIdentity(ctx, k[0], k[1], k[2], best); err != nil {
				return checked, identified, err
			}
			checked++
			if best != nil {
				identified++
			}
		}
	}
}
