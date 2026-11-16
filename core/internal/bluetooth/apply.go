package bluetooth

import (
	"context"
	"errors"
	"fmt"
	"github.com/MoriartyPuth/wraith/core/internal/btident"
	"strings"

	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// IsBluetooth reports whether an event type belongs to this package.
func IsBluetooth(eventType string) bool { return strings.HasPrefix(eventType, "bluetooth.") }

// Apply updates the Bluetooth inventory for one stored event, inside the
// ingest transaction. eventRow is the events.id of the stored event and
// seenMS Core's receive time. It returns the device row it wrote, or nil for
// events that do not describe a device (status, error, connection, GATT).
func Apply(ctx context.Context, tx *storage.Tx, ev *events.Event, eventRow, seenMS int64) (*storage.BTDevice, error) {
	_, cur, err := ApplyWithPrev(ctx, tx, ev, eventRow, seenMS)
	return cur, err
}

// ApplyWithPrev is Apply that also returns the device row as it was before
// this event (nil if the address was new or the event names no device), so
// detectors can compare before and after.
func ApplyWithPrev(ctx context.Context, tx *storage.Tx, ev *events.Event, eventRow, seenMS int64) (prev, cur *storage.BTDevice, err error) {
	var p *storage.BTDevice
	cur, err = apply(ctx, tx, ev, eventRow, seenMS, &p)
	return p, cur, err
}

func apply(ctx context.Context, tx *storage.Tx, ev *events.Event, eventRow, seenMS int64, prevOut **storage.BTDevice) (*storage.BTDevice, error) {
	switch ev.Type {
	case events.TypeBtClassicDiscovered, events.TypeBtClassicUpdated:
		var m ClassicMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		return upsert(ctx, tx, ev, eventRow, prevOut, Sighting{
			Transport: storage.BTClassic, Address: m.Address, Name: m.Name, CoD: m.CoD,
			DeviceClass: m.DeviceClass, RSSI: m.RSSI, Count: countOr(m.Count, 1),
			Seen: seenMS, Sensor: ev.SensorID,
		})

	case events.TypeBtBleAdvObserved:
		var m BLEAdvMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		rssi := m.RSSI
		return upsert(ctx, tx, ev, eventRow, prevOut, Sighting{
			Transport: storage.BTBLE, Address: m.Address, AddressType: m.AddressType,
			Name: m.Name, TxPower: m.TxPower, Services: m.Services,
			ManufacturerID: m.ManufacturerID, RSSI: &rssi, Count: countOr(m.Count, 1),
			Identity: decode(m.ManufacturerID, m.ManufacturerData, m.Services, seenMS), Decoded: true,
			Seen: seenMS, Sensor: ev.SensorID,
		})

	case events.TypeBtBleDiscovered, events.TypeBtBleUpdated:
		var m BLEDeviceMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		return upsert(ctx, tx, ev, eventRow, prevOut, Sighting{
			Transport: storage.BTBLE, Address: m.Address, AddressType: m.AddressType,
			Name: m.Name, Services: m.Services, ManufacturerID: m.ManufacturerID,
			Classification: m.Classification, Confidence: m.Confidence, RSSI: m.RSSI,
			// Device records carry no manufacturer data: only service UUIDs
			// (trackers, Fast Pair) and the company id can be recognised.
			Identity: decode(m.ManufacturerID, nil, m.Services, seenMS), Decoded: true,
			// A device record is a tracker snapshot; it only adds the
			// observations it explicitly represents (no implicit +1).
			Count: countOr(m.Count, 0),
			Seen:  seenMS, Sensor: ev.SensorID,
		})

	case events.TypeBtBleConnObserved, events.TypeBtBleConnLost,
		events.TypeBtGattService, events.TypeBtGattChar:
		// Operator-initiated GATT activity: link the event to the device's
		// history without changing the device row.
		var m AddressMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		addrType, err := resolveBLEAddressType(ctx, tx, m)
		if err != nil {
			return nil, err
		}
		return nil, tx.InsertBTObservation(ctx, storage.BTObservation{EventRow: eventRow,
			Transport: storage.BTBLE, Address: m.Address, AddressType: addrType,
			EventType: ev.Type, SensorID: ev.SensorID, ObservedAt: seenMS})

	case events.TypeBtSystemStatus:
		var m StatusMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		return nil, tx.UpsertBTStatus(ctx, storage.BTStatusUpdate{SensorID: ev.SensorID,
			Mode: m.Mode, State: m.State, ClassicDevices: m.ClassicDevices,
			BLEDevices: m.BLEDevices, Message: m.Message, At: seenMS})

	case events.TypeBtSystemError:
		var m ErrorMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return nil, err
		}
		return nil, tx.RecordBTError(ctx, ev.SensorID, m.Code, m.Message, seenMS)
	}
	// A bluetooth.* type the validator accepts but this switch does not know
	// would be a programming error, not bad sensor input.
	return nil, fmt.Errorf("bluetooth: unhandled event type %q", ev.Type)
}

func upsert(ctx context.Context, tx *storage.Tx, ev *events.Event, eventRow int64, prevOut **storage.BTDevice, s Sighting) (*storage.BTDevice, error) {
	var prev *storage.BTDevice
	p, err := tx.GetBTDevice(ctx, s.Transport, s.Address, s.AddressType)
	switch {
	case err == nil:
		prev = &p
		pc := p // Merge may share slices with prev; hand out an independent copy
		pc.Services = append([]string(nil), p.Services...)
		*prevOut = &pc
	case !errors.Is(err, storage.ErrNotFound):
		return nil, err
	}
	d := Merge(prev, s)
	if err := tx.PutBTDevice(ctx, d); err != nil {
		return nil, err
	}
	if err := tx.InsertBTObservation(ctx, storage.BTObservation{EventRow: eventRow,
		Transport: s.Transport, Address: s.Address, AddressType: s.AddressType,
		EventType: ev.Type, RSSI: s.RSSI, SensorID: s.Sensor, ObservedAt: s.Seen}); err != nil {
		return nil, err
	}
	return &d, nil
}

// resolveBLEAddressType uses the event's address_type when present, else the
// type of the most recently seen BLE device row with that address (GATT
// events carry no address type), else "".
func resolveBLEAddressType(ctx context.Context, tx *storage.Tx, m AddressMeta) (string, error) {
	if m.AddressType != nil {
		return *m.AddressType, nil
	}
	return tx.LatestBLEAddressType(ctx, m.Address)
}

// decode runs btident.Decode and stamps the identity with the receive time.
func decode(mfrID *int, mfrData *string, services []string, seenMS int64) *btident.Identity {
	id := btident.Decode(mfrID, mfrData, services)
	if id != nil {
		id.At = seenMS
	}
	return id
}

func countOr(c *int64, def int64) int64 {
	if c != nil {
		return *c
	}
	return def
}
