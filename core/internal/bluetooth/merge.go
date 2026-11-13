package bluetooth

import (
	"sort"
	"strings"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// MaxServices caps how many distinct service UUIDs one device row keeps.
const MaxServices = 32

// RSSIAlpha is the weight of a new RSSI sample in the rolling average.
const RSSIAlpha = 0.25

// Sighting is one event's contribution to a device row. Nil fields mean "not
// reported in this event" and leave the stored value as it was.
type Sighting struct {
	Transport, Address, AddressType string

	Name           *string
	CoD            *int
	DeviceClass    *string
	Classification *string
	Confidence     *float64
	ManufacturerID *int
	TxPower        *int
	Services       []string
	RSSI           *int

	// Count is how many observations this event represents and is added to
	// the device's total (docs/bluetooth/event-protocol.md, "Counting").
	Count int64

	Seen   int64 // Core receive time, Unix ms
	Sensor string
}

// Merge folds a sighting into the previous device row (nil for a new
// address) and returns the row to store. It never invents values: fields the
// sighting does not carry keep their previous value, and nothing is merged
// across addresses.
func Merge(prev *storage.BTDevice, s Sighting) storage.BTDevice {
	var d storage.BTDevice
	if prev != nil {
		d = *prev
	} else {
		d = storage.BTDevice{Transport: s.Transport, Address: s.Address,
			AddressType: s.AddressType, FirstSeen: s.Seen}
	}
	if s.Name != nil && *s.Name != "" { // names can change; latest non-empty wins
		d.Name = s.Name
	}
	if s.CoD != nil {
		d.CoD = s.CoD
	}
	switch {
	case s.DeviceClass != nil:
		d.DeviceClass = s.DeviceClass
	case s.CoD != nil:
		c := ClassFromCoD(*s.CoD)
		d.DeviceClass = &c
	}
	if s.Classification != nil {
		d.Classification, d.Confidence = s.Classification, s.Confidence
	}
	if s.ManufacturerID != nil {
		d.ManufacturerID = s.ManufacturerID
	}
	if s.TxPower != nil {
		d.TxPower = s.TxPower
	}
	d.Services = mergeServices(d.Services, s.Services)
	if s.RSSI != nil {
		r := *s.RSSI
		d.LastRSSI = &r
		avg := float64(r)
		if d.RSSIAvg != nil {
			avg = *d.RSSIAvg + RSSIAlpha*(float64(r)-*d.RSSIAvg)
		}
		d.RSSIAvg = &avg
	}
	if s.Count > 0 {
		d.Observations += s.Count
	}
	if s.Seen > d.LastSeen {
		d.LastSeen = s.Seen
	}
	if s.Sensor != "" {
		sensor := s.Sensor
		d.LastSensor = &sensor
	}
	return d
}

// mergeServices returns the sorted union of stored and newly advertised
// UUIDs (lowercased). Stored UUIDs are kept first when the cap is reached.
func mergeServices(have, add []string) []string {
	seen := make(map[string]bool, len(have)+len(add))
	out := make([]string, 0, len(have)+len(add))
	for _, list := range [][]string{have, add} {
		for _, u := range list {
			u = strings.ToLower(strings.TrimSpace(u))
			if u == "" || seen[u] || len(out) >= MaxServices {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}
