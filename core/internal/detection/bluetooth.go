package detection

import (
	"fmt"
	"github.com/MoriartyPuth/wraith/core/internal/btident"
	"slices"
	"strings"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/bluetooth"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// Bluetooth detectors (BT-10, docs/detection.md "Bluetooth"). They only use
// what the sensor observed; an address is an observation, not a device, and
// BLE random-address rotation is never treated as suspicious on its own.

// ---- ble_advertising_flood ----

const (
	floodWindow     = 60 * time.Second
	floodNewAddrs   = 30 // new BLE addresses per sensor per window
	floodPopupAddrs = 10 // of which carry a pairing pop-up signature
	floodCooldown   = 10 * time.Minute
	floodMaxTracked = 4096
)

const floodLimitations = "Busy places (stations, events) also produce many new addresses, because phones and " +
	"earbuds rotate their random BLE addresses; the threshold is a starting point, not a measured baseline. " +
	"The sensor's address table holds 64 entries, so during a flood it evicts and later rediscovers real devices, " +
	"which adds to the count. Pairing pop-up adverts are also sent by genuine earbuds and new phones — the " +
	"indicator is many distinct addresses doing it at once. Spoofed adverts cannot be attributed to a transmitter."

type floodAddr struct {
	at    time.Time
	popup bool
	evID  string
}

type bleFloodDetector struct {
	cd      *cooldown
	sensors map[string]map[string]*floodAddr // sensor -> address/type -> first sighting in window
}

func newBLEFloodDetector() *bleFloodDetector {
	return &bleFloodDetector{cd: newCooldown(floodCooldown), sensors: map[string]map[string]*floodAddr{}}
}

func (d *bleFloodDetector) Name() string { return "ble_advertising_flood" }

// popupSignature reports advertising data of a pairing pop-up mechanism that
// BLE "pop-up spam" tools imitate: Apple Continuity Proximity Pairing (0x07)
// and Nearby Action (0x0F), Microsoft Swift Pair (03 00 80), Google Fast Pair
// (service 0xFE2C).
func popupSignature(mfrID *int, mfrData *string, services []string) bool {
	if slices.Contains(services, "fe2c") {
		return true
	}
	if mfrID == nil || mfrData == nil {
		return false
	}
	data := strings.ToLower(*mfrData)
	switch *mfrID {
	case 0x004C:
		return strings.HasPrefix(data, "07") || strings.HasPrefix(data, "0f")
	case 0x0006:
		return strings.HasPrefix(data, "030080")
	}
	return false
}

func (d *bleFloodDetector) Observe(c *Context) []Finding {
	ev := c.Event
	var key string
	var popup bool
	isNew := false
	switch ev.Type {
	case events.TypeBtBleDiscovered:
		var m bluetooth.BLEDeviceMeta
		if ev.DecodeMeta(&m) != nil {
			return nil
		}
		key, isNew = m.AddressType+"/"+m.Address, true
		popup = popupSignature(nil, nil, m.Services)
	case events.TypeBtBleAdvObserved:
		var m bluetooth.BLEAdvMeta
		if ev.DecodeMeta(&m) != nil {
			return nil
		}
		key = m.AddressType + "/" + m.Address
		popup = popupSignature(m.ManufacturerID, m.ManufacturerData, m.Services)
	default:
		return nil
	}
	win := d.sensors[ev.SensorID]
	if win == nil {
		win = map[string]*floodAddr{}
		d.sensors[ev.SensorID] = win
	}
	for k, a := range win { // slide the window
		if c.Now.Sub(a.at) >= floodWindow {
			delete(win, k)
		}
	}
	if a := win[key]; a != nil {
		a.popup = a.popup || popup // an advert report can confirm a discovered address
	} else if isNew && len(win) < floodMaxTracked {
		win[key] = &floodAddr{at: c.Now, popup: popup, evID: ev.EventID}
	}
	if len(win) < floodNewAddrs {
		return nil
	}
	nPopup := 0
	var evidence []string
	for _, a := range win {
		if a.popup {
			nPopup++
		}
		evidence = append(evidence, a.evID)
	}
	if !d.cd.allow(ev.SensorID, c.Now) {
		return nil
	}
	slices.Sort(evidence)
	f := Finding{
		Category: "ble_advertising_flood", Severity: "medium", Confidence: 0.5, Classification: ClassSuspicious,
		Evidence: evidence, Limitations: floodLimitations,
		Explanation: fmt.Sprintf("Sensor %s heard %d new BLE addresses within %s (threshold %d); %d of them sent "+
			"pairing pop-up adverts.", ev.SensorID, len(win), floodWindow, floodNewAddrs, nPopup),
	}
	if nPopup >= floodPopupAddrs {
		f.Severity, f.Confidence, f.Classification = "high", 0.7, ClassPotential
		f.Explanation += fmt.Sprintf(" %d or more distinct addresses sending pairing pop-ups at once is the "+
			"pattern of BLE pop-up spam (fake AirPods / Fast Pair / Swift Pair adverts that flood nearby phones "+
			"with prompts).", floodPopupAddrs)
	}
	return []Finding{f}
}

// ---- bt_tracker_separated ----

const (
	trackerMinSpan  = 30 * time.Minute
	trackerMinSeen  = 3
	trackerForget   = 2 * time.Hour // Bluetooth sessions are ≤ 15 min; tolerate Wi-Fi-mode gaps
	trackerMaxTrack = 1024
)

const trackerLimitations = "A fixed sensor cannot tell whether a tracker is following anyone: this says an item " +
	"tracker that is away from its owner has stayed near the sensor. Neighbours' lost or stored items (keys, bags, " +
	"a car parked nearby) look the same. Recognised separated-mode adverts: Apple Find My (AirTag and other Find My " +
	"accessories) and the cross-platform unwanted-tracking alert (service 0xFCB2) that compliant Android-network " +
	"trackers send. Tile, Samsung SmartTag and Chipolo trackers are recognised but do not advertise a separated " +
	"state; they are covered by bt_tracker_lingering. The sensor only listens while in Bluetooth mode, so presence " +
	"between sessions is assumed, not observed."

type trackerSeen struct {
	first, last time.Time
	reports     int
	evidence    []string
	reported    bool
}

type trackerDetector struct{ seen map[string]*trackerSeen }

func newTrackerDetector() *trackerDetector { return &trackerDetector{seen: map[string]*trackerSeen{}} }

func (d *trackerDetector) Name() string { return "bt_tracker_separated" }

// separatedTracker decodes an advert and returns its identity when it is an
// item tracker broadcasting its away-from-owner state: Apple Find My's 25-byte
// Offline Finding payload (12 19…; the near-owner 12 02 form is ignored) or
// the cross-platform unwanted-tracking advert (service data 0xFCB2).
func separatedTracker(m *bluetooth.BLEAdvMeta) *btident.Identity {
	id := btident.Decode(m.ManufacturerID, m.ManufacturerData, m.Services)
	if id.IsTracker() && id.Separated {
		return id
	}
	return nil
}

func (d *trackerDetector) Observe(c *Context) []Finding {
	ev := c.Event
	if ev.Type != events.TypeBtBleAdvObserved {
		return nil
	}
	var m bluetooth.BLEAdvMeta
	if ev.DecodeMeta(&m) != nil {
		return nil
	}
	tid := separatedTracker(&m)
	if tid == nil {
		return nil
	}
	for k, s := range d.seen {
		if c.Now.Sub(s.last) > trackerForget {
			delete(d.seen, k)
		}
	}
	id := storage.BTDeviceID(storage.BTBLE, m.AddressType, m.Address)
	s := d.seen[id]
	if s == nil {
		if len(d.seen) >= trackerMaxTrack {
			return nil
		}
		s = &trackerSeen{first: c.Now}
		d.seen[id] = s
	}
	s.last = c.Now
	s.reports++
	if len(s.evidence) < 64 {
		s.evidence = append(s.evidence, ev.EventID)
	}
	if s.reported || s.reports < trackerMinSeen || s.last.Sub(s.first) < trackerMinSpan {
		return nil
	}
	s.reported = true
	return []Finding{{
		Category: "bt_tracker_separated", BTDevice: id, Severity: "medium", Confidence: 0.5,
		Classification: ClassSuspicious, Evidence: s.evidence, Limitations: trackerLimitations,
		Explanation: fmt.Sprintf("BLE address %s (%s) has broadcast a separated-from-owner tracker advert "+
			"(%s: %s) for %s (%d reports, first %s). An item tracker away from its owner that stays near the "+
			"sensor is the unwanted-tracking pattern; check whether it belongs to someone here.",
			m.Address, m.AddressType, tid.Label, tid.Source, s.last.Sub(s.first).Round(time.Minute), s.reports,
			s.first.UTC().Format(time.RFC3339)),
	}}
}

// ---- bt_tracker_lingering ----

// lingerMinSpan: how long one tracker address must have been around (first to
// latest sighting, across Bluetooth sessions) before it is reported.
const lingerMinSpan = 24 * time.Hour

const lingerLimitations = "A fixed sensor sees trackers that live nearby: a neighbour's keys, a tracker in a " +
	"parked car or a shared wall look the same as one hidden in your things. Only one address is followed: " +
	"trackers that rotate their address (AirTag every ~24 h, most others more often) restart the clock, so this " +
	"mainly catches trackers with a fixed address such as Tile. Samsung's 0xFD5A service is also used by Galaxy " +
	"phones and earbuds. Name a tracker you own (\"name it\") and it is not reported again. Presence between " +
	"Bluetooth sessions is assumed, not observed."

// trackerLingeringDetector reports an item tracker (any recognised brand)
// whose address has been around the sensor for at least lingerMinSpan and
// that the operator has not named. Separated-mode trackers are left to
// bt_tracker_separated. Once per device row, also across Core restarts.
type trackerLingeringDetector struct{ done map[string]bool }

func newTrackerLingeringDetector() *trackerLingeringDetector {
	return &trackerLingeringDetector{done: map[string]bool{}}
}

func (d *trackerLingeringDetector) Name() string { return "bt_tracker_lingering" }

func (d *trackerLingeringDetector) Observe(c *Context) []Finding {
	cur := c.CurBT
	if cur == nil || cur.Transport != storage.BTBLE || !cur.Identity.IsTracker() || cur.Identity.Separated ||
		cur.Label != "" || d.done[cur.ID] {
		return nil
	}
	span := time.Duration(cur.LastSeen-cur.FirstSeen) * time.Millisecond
	if span < lingerMinSpan {
		return nil
	}
	d.done[cur.ID] = true
	if c.Lookup != nil {
		if seen, err := c.Lookup.BTFindingExists(d.Name(), cur.ID); err != nil || seen {
			return nil
		}
	}
	id := cur.Identity
	return []Finding{{
		Category: "bt_tracker_lingering", BTDevice: cur.ID, Severity: "low", Confidence: 0.4,
		Classification: ClassSuspicious, Evidence: []string{c.Event.EventID}, Limitations: lingerLimitations,
		Explanation: fmt.Sprintf("An item tracker nobody here has named (%s, from %s) has been around the sensor "+
			"for %s: BLE address %s (%s) first seen %s, last seen %s, %d observations. If it is yours, name it; "+
			"if not, look for it among things that entered the space recently.",
			id.Label, id.Source, span.Round(time.Hour), cur.Address, cur.AddressType,
			time.UnixMilli(cur.FirstSeen).UTC().Format(time.RFC3339),
			time.UnixMilli(cur.LastSeen).UTC().Format(time.RFC3339), cur.Observations),
	}}
}

// ---- bt_identity_change ----

const identityLimitations = "Owners rename devices and firmware updates change names; a device class change is " +
	"rarer but some devices switch profiles. The same address advertising a different identity is also what " +
	"address spoofing looks like — compare with the device itself before acting."

type btIdentityDetector struct{ cd *cooldown }

func newBTIdentityDetector() *btIdentityDetector {
	return &btIdentityDetector{cd: newCooldown(time.Hour)}
}

func (d *btIdentityDetector) Name() string { return "bt_identity_change" }

func (d *btIdentityDetector) Observe(c *Context) []Finding {
	prev, cur := c.PrevBT, c.CurBT
	if prev == nil || cur == nil {
		return nil
	}
	// Only stable addresses: Classic BD_ADDRs and BLE public addresses. A
	// random address is not an identity.
	if cur.Transport != storage.BTClassic && cur.AddressType != "public" {
		return nil
	}
	var out []Finding
	pn, cn := deref(prev.Name), deref(cur.Name)
	if pn != "" && cn != "" && pn != cn && !strings.HasPrefix(cn, pn) && !strings.HasPrefix(pn, cn) &&
		d.cd.allow(cur.ID+"/name", c.Now) {
		out = append(out, Finding{
			Category: "bt_identity_change", BTDevice: cur.ID, Severity: "low", Confidence: 0.6,
			Classification: ClassObserved, Evidence: []string{c.Event.EventID}, Limitations: identityLimitations,
			Explanation: fmt.Sprintf("Bluetooth %s address %s changed its name from %q to %q.",
				cur.Transport, cur.Address, pn, cn),
		})
	}
	pc, cc := deref(prev.DeviceClass), deref(cur.DeviceClass)
	if pc != "" && cc != "" && pc != cc && d.cd.allow(cur.ID+"/class", c.Now) {
		out = append(out, Finding{
			Category: "bt_identity_change", BTDevice: cur.ID, Severity: "low", Confidence: 0.4,
			Classification: ClassSuspicious, Evidence: []string{c.Event.EventID}, Limitations: identityLimitations,
			Explanation: fmt.Sprintf("Bluetooth Classic address %s changed its advertised device class from %s to %s "+
				"(Class of Device %s).", cur.Address, pc, cc, codText(cur.CoD)),
		})
	}
	return out
}

func codText(c *int) string {
	if c == nil {
		return "unknown"
	}
	return fmt.Sprintf("0x%06X", *c)
}
