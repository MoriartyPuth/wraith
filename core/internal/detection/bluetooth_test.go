package detection

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/btident"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

func btEv(typ string, meta map[string]any) *events.Event {
	e := ev(typ, "", meta)
	e.Version = 4
	return e
}

func addr(i int) string { return fmt.Sprintf("4A:00:00:00:%02X:%02X", i/256, i%256) }

func discovered(i int, services ...string) *events.Event {
	m := map[string]any{"address": addr(i), "address_type": "random", "rssi": -70, "count": 1}
	if len(services) > 0 {
		m["services"] = services
	}
	return btEv(events.TypeBtBleDiscovered, m)
}

func advert(a, mfrData string, mfrID int) *events.Event {
	return btEv(events.TypeBtBleAdvObserved, map[string]any{"address": a, "address_type": "random", "rssi": -60,
		"manufacturer_id": mfrID, "manufacturer_data": mfrData, "count": 3, "window_ms": 10000})
}

func only(fs []storage.Finding, cat string) []storage.Finding {
	var out []storage.Finding
	for _, f := range fs {
		if f.Category == cat {
			out = append(out, f)
		}
	}
	return out
}

func TestBLEFloodThresholdAndWindow(t *testing.T) {
	e := NewEngine()
	t0 := time.Now()
	// 29 new addresses in a minute (a busy but normal area) — silent; old
	// ones leaving the window keep it silent as new ones arrive.
	for i := 0; i < 60; i++ {
		f := e.Observe(&Context{Event: discovered(i), Now: t0.Add(time.Duration(i) * 2100 * time.Millisecond)})
		if len(f) != 0 {
			t.Fatalf("finding at %d new addresses/min: %+v", i, f[0])
		}
	}
	// Repeated reports for known addresses are not new addresses.
	for i := 0; i < 100; i++ {
		if f := e.Observe(&Context{Event: discovered(59), Now: t0.Add(130 * time.Second)}); len(f) != 0 {
			t.Fatal("one address counted many times")
		}
	}
	// 30 in one minute: suspicious, not an attack (no pop-ups).
	e = NewEngine()
	var got []storage.Finding
	for i := 0; i < 30; i++ {
		got = append(got, e.Observe(&Context{Event: discovered(1000 + i), Now: t0.Add(time.Duration(i) * time.Second)})...)
	}
	if len(got) != 1 || got[0].Category != "ble_advertising_flood" || got[0].Classification != ClassSuspicious ||
		got[0].Severity != "medium" || len(got[0].Evidence) != 30 || got[0].BTDevice != nil || got[0].BSSID != nil {
		t.Fatalf("flood finding: %+v", got)
	}
	if !strings.Contains(got[0].Explanation, "30 new BLE addresses") || got[0].Limitations == "" {
		t.Fatalf("explanation: %s", got[0].Explanation)
	}
	// Cooldown: the flood continuing does not repeat the finding.
	for i := 30; i < 200; i++ {
		if f := e.Observe(&Context{Event: discovered(1000 + i), Now: t0.Add(time.Duration(i) * time.Second / 4)}); len(f) != 0 {
			t.Fatal("finding repeated within the cooldown")
		}
	}
}

func TestBLEFloodPopupSpam(t *testing.T) {
	e := NewEngine()
	t0 := time.Now()
	var got []storage.Finding
	obs := func(ev *events.Event, i int) {
		got = append(got, e.Observe(&Context{Event: ev, Now: t0.Add(time.Duration(i) * 100 * time.Millisecond)})...)
	}
	// 9 spoofed AirPods (Proximity Pairing), confirmed by their advert reports,
	// 1 Fast Pair via its service in the discovery, then plain addresses.
	for i := 0; i < 9; i++ {
		obs(discovered(i), i)
		obs(advert(addr(i), "0719010e2001", 0x004C), i)
	}
	obs(discovered(9, "fe2c"), 9)
	// Not pop-ups: Apple Nearby Info, Find My, Microsoft CDP, an unknown
	// address's report (only discovered addresses are counted).
	obs(advert(addr(20), "1007", 0x004C), 10)
	obs(advert("4B:00:00:00:00:01", "0f05", 0x004C), 10)
	obs(advert(addr(21), "0109", 0x0006), 10)
	obs(discovered(40), 10)
	obs(advert(addr(40), "0301ff", 0x0006), 10) // Microsoft, but not Swift Pair (03 00 80)
	for i := 10; i < 28; i++ {                  // + addr 40 above = 29 new addresses
		obs(discovered(i), 11)
	}
	if len(got) != 0 {
		t.Fatal("finding below the address threshold")
	}
	obs(discovered(29), 12)
	if len(got) != 1 || got[0].Classification != ClassPotential || got[0].Severity != "high" ||
		!strings.Contains(got[0].Explanation, "10 of them sent pairing pop-up") {
		t.Fatalf("pop-up spam: %+v", got)
	}

	// Microsoft Swift Pair counts; one pop-up short stays suspicious.
	e = NewEngine()
	got = nil
	for i := 0; i < 30; i++ {
		obs(discovered(100+i), i)
		if i < 9 {
			obs(advert(addr(100+i), "030080", 0x0006), i)
		}
	}
	if len(got) != 1 || got[0].Classification != ClassSuspicious {
		t.Fatalf("9 pop-ups: %+v", got)
	}
}

func TestTrackerSeparated(t *testing.T) {
	e := NewEngine()
	t0 := time.Now()
	const tag = "C3:11:22:33:44:55"
	sep := func(at time.Duration, a string) []storage.Finding {
		return e.Observe(&Context{Event: advert(a, "1219"+strings.Repeat("ab", 22), 0x004C), Now: t0.Add(at)})
	}
	// Near-owner form (12 02) never counts — the common case on real data.
	for i := 0; i < 50; i++ {
		if f := e.Observe(&Context{Event: advert("D0:00:00:00:00:01", "1202e4", 0x004C), Now: t0.Add(time.Duration(i) * time.Minute)}); len(f) != 0 {
			t.Fatal("near-owner Find My advert flagged")
		}
	}
	// Separated across three 15-minute Bluetooth sessions with gaps.
	var got []storage.Finding
	for _, at := range []time.Duration{0, 10 * time.Second, 14 * time.Minute, 29 * time.Minute} {
		got = append(got, sep(at, tag)...)
	}
	if len(got) != 0 {
		t.Fatal("finding before a 30 min span")
	}
	got = sep(45*time.Minute, tag) // span 45 min ≥ 30, 5 reports
	if len(got) != 1 || got[0].Category != "bt_tracker_separated" || got[0].BTDevice == nil ||
		*got[0].BTDevice != "ble:random:"+tag || got[0].Classification != ClassSuspicious || len(got[0].Evidence) != 5 {
		t.Fatalf("tracker: %+v", got)
	}
	if f := sep(50*time.Minute, tag); len(f) != 0 {
		t.Fatal("tracker reported twice")
	}
	// Seen once, then again after the forget horizon: starts over.
	sep(0, "C3:00:00:00:00:02")
	sep(time.Minute, "C3:00:00:00:00:02")
	if f := sep(3*time.Hour, "C3:00:00:00:00:02"); len(f) != 0 {
		t.Fatal("span bridged a gap longer than the forget horizon")
	}
	// Long span but too few reports.
	sep(0, "C3:00:00:00:00:03")
	if f := sep(40*time.Minute, "C3:00:00:00:00:03"); len(f) != 0 {
		t.Fatal("two reports are not presence")
	}
}

func btDev(transport, addrType, address, name, class string) *storage.BTDevice {
	d := &storage.BTDevice{ID: storage.BTDeviceID(transport, addrType, address), Transport: transport,
		AddressType: addrType, Address: address}
	if name != "" {
		d.Name = &name
	}
	if class != "" {
		d.DeviceClass = &class
		cod := 0x5A020C
		d.CoD = &cod
	}
	return d
}

func TestBTIdentityChange(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	evt := btEv(events.TypeBtClassicUpdated, map[string]any{"address": "AA:00:00:00:00:01", "count": 1})
	obs := func(prev, cur *storage.BTDevice) []storage.Finding {
		now = now.Add(2 * time.Hour) // outside the cooldown
		return only(e.Observe(&Context{Event: evt, PrevBT: prev, CurBT: cur, Now: now}), "bt_identity_change")
	}
	const A = "AA:00:00:00:00:01"
	f := obs(btDev(storage.BTClassic, "", A, "Phone", "phone"), btDev(storage.BTClassic, "", A, "JBL Flip 5", "phone"))
	if len(f) != 1 || f[0].Classification != ClassObserved || *f[0].BTDevice != "classic:"+A ||
		!strings.Contains(f[0].Explanation, `"Phone" to "JBL Flip 5"`) {
		t.Fatalf("name change: %+v", f)
	}
	f = obs(btDev(storage.BTClassic, "", A, "Phone", "phone"), btDev(storage.BTClassic, "", A, "Phone", "audio"))
	if len(f) != 1 || f[0].Classification != ClassSuspicious || !strings.Contains(f[0].Explanation, "phone to audio") {
		t.Fatalf("class change: %+v", f)
	}
	for _, tc := range []struct {
		what      string
		prev, cur *storage.BTDevice
	}{
		{"new device", nil, btDev(storage.BTClassic, "", A, "Phone", "phone")},
		{"shortened -> complete", btDev(storage.BTClassic, "", A, "JBL", ""), btDev(storage.BTClassic, "", A, "JBL Flip 5", "")},
		{"complete -> shortened", btDev(storage.BTClassic, "", A, "JBL Flip 5", ""), btDev(storage.BTClassic, "", A, "JBL", "")},
		{"name learned", btDev(storage.BTClassic, "", A, "", "phone"), btDev(storage.BTClassic, "", A, "Phone", "phone")},
		{"class learned", btDev(storage.BTClassic, "", A, "Phone", ""), btDev(storage.BTClassic, "", A, "Phone", "phone")},
		{"random BLE address", btDev(storage.BTBLE, "random", A, "Band", ""), btDev(storage.BTBLE, "random", A, "Watch", "")},
	} {
		if f := obs(tc.prev, tc.cur); len(f) != 0 {
			t.Errorf("%s: unexpected finding %+v", tc.what, f)
		}
	}
	// BLE public addresses are stable identities too.
	if f := obs(btDev(storage.BTBLE, "public", A, "Band", ""), btDev(storage.BTBLE, "public", A, "Watch", "")); len(f) != 1 {
		t.Fatal("public BLE rename not reported")
	}
	// Cooldown: flapping names within the hour report once.
	e = NewEngine()
	n := 0
	for i := 0; i < 10; i++ {
		a, b := "Alpha", "Beta"
		if i%2 == 1 {
			a, b = b, a
		}
		n += len(only(e.Observe(&Context{Event: evt, PrevBT: btDev(storage.BTClassic, "", A, a, ""),
			CurBT: btDev(storage.BTClassic, "", A, b, ""), Now: now.Add(time.Duration(i) * time.Minute)}), "bt_identity_change"))
	}
	if n != 1 {
		t.Fatalf("flapping name produced %d findings", n)
	}
}

// Wi-Fi events never reach the Bluetooth detectors' state, and Bluetooth
// events never produce Wi-Fi findings.
func TestBluetoothDetectorsIgnoreWifi(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	for i := 0; i < 100; i++ {
		f := e.Observe(&Context{Event: ev(events.TypeAPObserved, fmt.Sprintf("AA:BB:CC:00:00:%02X", i),
			map[string]any{"ssid": "x"}), Now: now})
		for _, x := range f {
			if x.BTDevice != nil || strings.HasPrefix(x.Category, "b") {
				t.Fatalf("Wi-Fi event produced %+v", x)
			}
		}
	}
}

// TestTrackerSeparatedDULT: the cross-platform unwanted-tracking advert
// (service data 0xFCB2) counts as separated for any brand.
func TestTrackerSeparatedDULT(t *testing.T) {
	e := NewEngine()
	t0 := time.Now()
	const a = "C4:00:00:00:00:01"
	var got []storage.Finding
	for _, at := range []time.Duration{0, 15 * time.Minute, 31 * time.Minute} {
		ev := btEv(events.TypeBtBleAdvObserved, map[string]any{"address": a, "address_type": "random", "rssi": -70,
			"services": []string{"fcb2"}, "count": 1})
		got = append(got, only(e.Observe(&Context{Event: ev, Now: t0.Add(at)}), "bt_tracker_separated")...)
	}
	if len(got) != 1 || !strings.Contains(got[0].Explanation, "0xfcb2") {
		t.Fatalf("DULT separated: %+v", got)
	}
	// A recognised tracker without a separated state (Tile) is not "separated".
	for _, at := range []time.Duration{0, 15 * time.Minute, 31 * time.Minute, 60 * time.Minute} {
		ev := btEv(events.TypeBtBleAdvObserved, map[string]any{"address": "C4:00:00:00:00:02", "address_type": "random",
			"rssi": -70, "services": []string{"feed"}, "count": 1})
		if f := only(e.Observe(&Context{Event: ev, Now: t0.Add(at)}), "bt_tracker_separated"); len(f) != 0 {
			t.Fatalf("Tile reported as separated: %+v", f)
		}
	}
}

func TestTrackerLingering(t *testing.T) {
	t0 := time.Now()
	tile := &btident.Identity{Kind: btident.KindTracker, Label: "Tile tracker", Source: "service UUID 0xfeed (Tile)",
		Rank: 85, Tracker: btident.TrackerTile}
	dev := func(addr string, span time.Duration, id *btident.Identity, label string) *storage.BTDevice {
		return &storage.BTDevice{ID: "ble:public:" + addr, Transport: storage.BTBLE, Address: addr, AddressType: "public",
			FirstSeen: t0.Add(-span).UnixMilli(), LastSeen: t0.UnixMilli(), Observations: 42, Identity: id, Label: label}
	}
	run := func(e *Engine, lk Lookup, d *storage.BTDevice) []storage.Finding {
		ev := btEv(events.TypeBtBleAdvObserved, map[string]any{"address": d.Address, "address_type": "public", "rssi": -70})
		return only(e.Observe(&Context{Event: ev, CurBT: d, Lookup: lk, Now: t0}), "bt_tracker_lingering")
	}
	lk := &fakeLookup{reported: map[string]bool{}}
	e := NewEngine()

	if f := run(e, lk, dev("00:11:22:00:00:01", 23*time.Hour, tile, "")); len(f) != 0 {
		t.Fatal("reported before 24 h")
	}
	f := run(e, lk, dev("00:11:22:00:00:01", 25*time.Hour, tile, ""))
	if len(f) != 1 || *f[0].BTDevice != "ble:public:00:11:22:00:00:01" || f[0].Severity != "low" ||
		!strings.Contains(f[0].Explanation, "Tile tracker") || !strings.Contains(f[0].Explanation, "25h") {
		t.Fatalf("lingering: %+v", f)
	}
	if f := run(e, lk, dev("00:11:22:00:00:01", 26*time.Hour, tile, "")); len(f) != 0 {
		t.Fatal("reported twice")
	}
	for name, d := range map[string]*storage.BTDevice{
		"named by operator": dev("00:11:22:00:00:02", 48*time.Hour, tile, "my keys"),
		"separated (other detector)": dev("00:11:22:00:00:03", 48*time.Hour,
			&btident.Identity{Kind: btident.KindTracker, Separated: true, Tracker: btident.TrackerDULT}, ""),
		"near-owner Find My": dev("00:11:22:00:00:04", 48*time.Hour,
			&btident.Identity{Kind: btident.KindFindMy, Tracker: btident.TrackerApple}, ""),
		"not a tracker": dev("00:11:22:00:00:05", 48*time.Hour, &btident.Identity{Kind: btident.KindPhone}, ""),
		"no identity":   dev("00:11:22:00:00:06", 48*time.Hour, nil, ""),
	} {
		if f := run(e, lk, d); len(f) != 0 {
			t.Errorf("%s: reported %+v", name, f)
		}
	}
	// Already reported before a Core restart (stored finding): not again.
	lk.reported["bt_tracker_lingering/ble:public:00:11:22:00:00:07"] = true
	if f := run(NewEngine(), lk, dev("00:11:22:00:00:07", 48*time.Hour, tile, "")); len(f) != 0 {
		t.Fatal("re-reported after restart")
	}
}
