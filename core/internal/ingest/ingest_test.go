package ingest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/bluetooth"
	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/detection"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/stream"
)

type recPub struct {
	mu   sync.Mutex
	msgs map[string]int
	last map[string]any
}

func (r *recPub) Publish(kind string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs[kind]++
	r.last[kind] = data
}

func setup(t *testing.T) (*Pipeline, *storage.Store, *recPub) {
	t.Helper()
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	pub := &recPub{msgs: map[string]int{}, last: map[string]any{}}
	p := New(st, detection.NewEngine(), pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return p, st, pub
}

// line builds and validates a Nano event exactly as it would arrive.
func line(t *testing.T, boot string, seq int, typ, extra, meta string) *events.Event {
	t.Helper()
	raw := fmt.Sprintf(`{"version":1,"event_id":"%s-%d","source":"wraith-nano","sensor_id":"nano-70b2a8","event_type":"%s","timestamp":0,"seq":%d%s,"metadata":%s}`,
		boot, seq, typ, seq, extra, meta)
	ev, err := events.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("fixture invalid: %v\n%s", err, raw)
	}
	return ev
}

const B = "AA:BB:CC:00:00:01"

func feed(t *testing.T, p *Pipeline, evs ...*events.Event) {
	t.Helper()
	for _, e := range evs {
		if err := p.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSensorLifecycleAndAPInventory(t *testing.T) {
	p, st, pub := setup(t)
	ctx := context.Background()
	feed(t, p,
		line(t, "aaaa0001", 0, "sensor.status", "", `{"state":"booting","firmware_version":"0.1.0","board":"esp32dev","capabilities":["serial-json"]}`),
		line(t, "aaaa0001", 1, "sensor.status", "", `{"state":"online","firmware_version":"0.1.0","board":"esp32dev","capabilities":["serial-json","wifi-2.4ghz"]}`),
		line(t, "aaaa0001", 2, "wifi.ap_observed", `,"channel":6,"bssid":"`+B+`"`, `{"ssid":"Lab","ssid_hex":"4c6162","rssi":-50,"auth_mode":"wpa2-psk","hidden":false}`),
		line(t, "aaaa0001", 3, "wifi.pmf_observed", `,"bssid":"`+B+`"`, `{"pmf":"capable","rsn_present":true}`),
		line(t, "aaaa0001", 4, "wifi.beacon_observed", `,"channel":6,"bssid":"`+B+`"`,
			`{"ssid":"Lab","ssid_hex":"4c6162","rssi":-48,"beacon_interval":100,"capability":1041,"rsn":{"version":1,"group_cipher":"ccmp-128","pairwise":["ccmp-128"],"akm":["psk"],"mfpc":true,"mfpr":false},"count":1}`),
		line(t, "aaaa0001", 5, "sensor.heartbeat", "", `{"uptime_ms":5000,"free_heap":165000,"channel":6,"queue_drops":0}`),
	)

	s, err := st.GetSensor(ctx, "nano-70b2a8")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != "online" || *s.FirmwareVersion != "0.1.0" || *s.BootID != "aaaa0001" || s.LastHeartbeat == nil || *s.CurrentChannel != 6 {
		t.Fatalf("sensor: %+v", s)
	}
	a, err := st.GetAccessPoint(ctx, B)
	if err != nil {
		t.Fatal(err)
	}
	if a.SSID != "Lab" || *a.AuthMode != "wpa2-psk" || a.PMF != "capable" || *a.LastRSSI != -48 || *a.LastChannel != 6 {
		t.Fatalf("ap: %+v", a)
	}
	if string(a.RSN) == "null" || len(a.RSN) == 0 {
		t.Fatalf("rsn not stored: %s", a.RSN)
	}
	obs, _ := st.ListObservations(ctx, B, 10)
	if len(obs) != 2 {
		t.Fatalf("observations: %+v", obs)
	}
	_, total, _ := st.ListEvents(ctx, storage.Page{Limit: 10}, storage.EventFilter{})
	if total != 6 || p.Ingested.Load() != 6 {
		t.Fatalf("events stored %d, ingested %d", total, p.Ingested.Load())
	}
	if pub.msgs[stream.KindEvent] != 6 || pub.msgs[stream.KindAP] != 3 || pub.msgs[stream.KindSensor] != 3 {
		t.Fatalf("published: %v", pub.msgs)
	}

	// Beacon without RSN clears the stored RSN (AP stopped advertising it).
	feed(t, p, line(t, "aaaa0001", 6, "wifi.beacon_observed", `,"channel":6,"bssid":"`+B+`"`, `{"ssid":"Lab","ssid_hex":"4c6162","rssi":-48,"count":3}`))
	a, _ = st.GetAccessPoint(ctx, B)
	if string(a.RSN) != "null" {
		t.Fatalf("rsn should be cleared, got %s", a.RSN)
	}
}

func TestDuplicatesGapsRestarts(t *testing.T) {
	p, st, _ := setup(t)
	ctx := context.Background()
	hb := func(boot string, seq int) *events.Event {
		return line(t, boot, seq, "sensor.heartbeat", "", `{"uptime_ms":1,"free_heap":1,"queue_drops":0}`)
	}
	feed(t, p, hb("aaaa0001", 0), hb("aaaa0001", 1), hb("aaaa0001", 1), hb("aaaa0001", 5), hb("bbbb0002", 0))
	s, _ := st.GetSensor(ctx, "nano-70b2a8")
	if s.DuplicateCount != 1 || s.GapCount != 3 || *s.BootID != "bbbb0002" {
		t.Fatalf("counters: dup=%d gap=%d boot=%v", s.DuplicateCount, s.GapCount, *s.BootID)
	}
	if p.Restarts.Load() != 1 {
		t.Fatalf("restarts = %d", p.Restarts.Load())
	}

	// Core restart: a fresh pipeline (empty LRU) receives an already-stored
	// event; DB uniqueness still catches it.
	p2 := New(st, detection.NewEngine(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := p2.Handle(ctx, hb("bbbb0002", 0)); err != nil {
		t.Fatal(err)
	}
	s, _ = st.GetSensor(ctx, "nano-70b2a8")
	if s.DuplicateCount != 2 || p2.Ingested.Load() != 0 {
		t.Fatalf("db-level duplicate not caught: dup=%d ingested=%d", s.DuplicateCount, p2.Ingested.Load())
	}
}

func TestRejectionsCountedPerSensor(t *testing.T) {
	p, st, _ := setup(t)
	p.Rejected(capture.Rejection{Provider: "esp32-serial", SensorID: "nano-x", Reason: "invalid_field"})
	p.Rejected(capture.Rejection{Provider: "esp32-serial", SensorID: "", Reason: "oversize"}) // unattributable
	s, err := st.GetSensor(context.Background(), "nano-x")
	if err != nil || s.RejectedCount != 1 {
		t.Fatalf("rejected: %+v %v", s, err)
	}
}

func TestDeauthBurstProducesStoredFinding(t *testing.T) {
	p, st, pub := setup(t)
	ctx := context.Background()
	base := time.Now()
	p.now = func() time.Time { return base }
	for i := 0; i < 4; i++ {
		e := line(t, "aaaa0001", i, "wifi.deauth_observed", `,"channel":6,"bssid":"`+B+`"`,
			`{"count":10,"window_ms":1000,"src":"`+B+`","dst":"FF:FF:FF:FF:FF:FF","reason_codes":[7],"rssi":-40,"protected":false}`)
		e.ReceivedAt = base.Add(time.Duration(i) * time.Second)
		feed(t, p, e)
	}
	fs, total, err := st.ListFindings(ctx, storage.Page{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("findings: %d %v", total, err)
	}
	f := fs[0]
	if f.Category != "deauth_activity" || f.Classification != "suspicious" || len(f.Evidence) != 3 || f.Limitations == "" {
		t.Fatalf("finding: %+v", f)
	}
	if pub.msgs[stream.KindFinding] != 1 {
		t.Fatalf("finding not published: %v", pub.msgs)
	}
	// Evidence IDs resolve to the stored events.
	evs, n, _ := st.ListEvents(ctx, storage.Page{Limit: 64}, storage.EventFilter{EventIDs: f.Evidence})
	if n != len(f.Evidence) || len(evs) != n {
		t.Fatalf("evidence lookup: %d of %d", n, len(f.Evidence))
	}
	// Deauth events reference a BSSID but must not create AP inventory rows.
	if _, err := st.GetAccessPoint(ctx, B); err != storage.ErrNotFound {
		t.Fatalf("deauth created an AP row: %v", err)
	}
	// Review workflow.
	got, err := st.SetFindingStatus(ctx, f.ID, "reviewed", base.UnixMilli())
	if err != nil || got.Status != "reviewed" {
		t.Fatalf("review: %+v %v", got, err)
	}
}

func TestRogueIndicatorThroughPipeline(t *testing.T) {
	p, st, _ := setup(t)
	p.start = time.Now().Add(-10 * time.Minute) // past the rogue detector warm-up
	ap := func(seq int, bssid, auth string) *events.Event {
		return line(t, "aaaa0001", seq, "wifi.ap_observed", `,"channel":6,"bssid":"`+bssid+`"`,
			`{"ssid":"Home","ssid_hex":"486f6d65","rssi":-50,"auth_mode":"`+auth+`","hidden":false}`)
	}
	feed(t, p, ap(0, "AA:00:00:00:00:01", "wpa2-psk"), ap(1, "AA:00:00:00:00:02", "wpa2-psk"))
	_, n, _ := st.ListFindings(context.Background(), storage.Page{Limit: 10})
	if n != 0 {
		t.Fatalf("mesh pair flagged")
	}
	feed(t, p, ap(2, "02:00:00:00:00:99", "open"))
	fs, n, _ := st.ListFindings(context.Background(), storage.Page{Limit: 10})
	if n != 1 || fs[0].Category != "rogue_ap_indicator" || *fs[0].BSSID != "02:00:00:00:00:99" {
		t.Fatalf("rogue: %+v", fs)
	}
}

func TestFindingDedupAcrossRestart(t *testing.T) {
	p, st, _ := setup(t)
	p.start = time.Now().Add(-10 * time.Minute)
	ap := func(seq int, bssid, auth string) *events.Event {
		return line(t, "aaaa0001", seq, "wifi.ap_observed", `,"channel":6,"bssid":"`+bssid+`"`,
			`{"ssid":"Home","ssid_hex":"486f6d65","rssi":-50,"auth_mode":"`+auth+`","hidden":false}`)
	}
	// First run: strong pair then an open twin -> one rogue finding.
	feed(t, p, ap(0, "AA:00:00:00:00:01", "wpa2-psk"), ap(1, "02:00:00:00:00:99", "open"))
	_, n, _ := st.ListFindings(context.Background(), storage.Page{Limit: 10})
	if n != 1 {
		t.Fatalf("first run findings = %d, want 1", n)
	}
	// Simulate a Core restart: fresh pipeline (empty detector + dedup state),
	// same store, re-observe the same APs.
	// New boot_id + higher seq => genuinely new events (a real restarted
	// sensor keeps counting); only the finding would be a duplicate.
	ap2 := func(seq int, bssid, auth string) *events.Event {
		return line(t, "bbbb0002", seq, "wifi.ap_observed", `,"channel":6,"bssid":"`+bssid+`"`,
			`{"ssid":"Home","ssid_hex":"486f6d65","rssi":-50,"auth_mode":"`+auth+`","hidden":false}`)
	}
	p2 := New(st, detection.NewEngine(), nil, p.log)
	p2.start = time.Now().Add(-10 * time.Minute)
	feed(t, p2, ap2(0, "AA:00:00:00:00:01", "wpa2-psk"), ap2(1, "02:00:00:00:00:99", "open"))
	_, n, _ = st.ListFindings(context.Background(), storage.Page{Limit: 10})
	if n != 1 {
		t.Fatalf("after restart findings = %d, want 1 (duplicate not suppressed)", n)
	}
	if p2.Deduped.Load() == 0 {
		t.Fatal("dedup counter not incremented")
	}
}

func TestClientSighting(t *testing.T) {
	p, st, pub := setup(t)
	ctx := context.Background()
	const bssid = "64:13:AB:A5:53:E8"
	const phone = "F6:C1:32:28:B8:85"
	v3 := func(seq int, typ, extra, meta string) *events.Event {
		raw := fmt.Sprintf(`{"version":3,"event_id":"cc001122-%d","source":"wraith-nano","sensor_id":"nano-70b2a8","event_type":"%s","timestamp":0,"seq":%d%s,"metadata":%s}`,
			seq, typ, seq, extra, meta)
		ev, err := events.Parse([]byte(raw))
		if err != nil {
			t.Fatalf("fixture invalid: %v\n%s", err, raw)
		}
		return ev
	}
	feed(t, p,
		v3(0, "wifi.ap_observed", `,"channel":11,"bssid":"`+bssid+`"`,
			`{"ssid":"Bo","ssid_hex":"426f","rssi":-60,"auth_mode":"wpa-wpa2-psk"}`),
		v3(1, "wifi.client_observed", `,"channel":11,"bssid":"`+bssid+`"`,
			`{"client":"`+phone+`","frames":12,"window_ms":10000,"rssi":-55}`),
	)
	c, err := st.GetClient(ctx, bssid, phone)
	if err != nil {
		t.Fatal(err)
	}
	if c.Client != phone || c.Frames != 12 || c.SSID != "Bo" || c.LastRSSI == nil || *c.LastRSSI != -55 {
		t.Fatalf("client: %+v", c)
	}
	if pub.msgs["client"] != 1 {
		t.Fatalf("client stream messages: %v", pub.msgs)
	}
	// A later burst accumulates frames and keeps first_seen.
	feed(t, p, v3(2, "wifi.client_observed", `,"channel":11,"bssid":"`+bssid+`"`,
		`{"client":"`+phone+`","frames":5,"window_ms":10000}`))
	c, _ = st.GetClient(ctx, bssid, phone)
	if c.Frames != 17 {
		t.Fatalf("frames accumulate: %+v", c)
	}
	cs, total, err := st.ListClients(ctx, storage.Page{Limit: 10}, bssid)
	if err != nil || total != 1 || len(cs) != 1 || cs[0].Client != phone {
		t.Fatalf("list clients: %v %d %v", err, total, cs)
	}
}

// Protocol v4: Bluetooth events from the switch-mode firmware build must be
// accepted and stored as generic events, without touching Wi-Fi state (no AP
// rows, no RSSI observations, no Wi-Fi findings) and without ingest failures.
// Bluetooth-specific inventory arrives in a later phase; until then the event
// history is the record.
func TestBluetoothEventsStoredWithoutTouchingWifi(t *testing.T) {
	p, st, pub := setup(t)
	ctx := context.Background()
	v4 := func(seq int, typ, meta string) *events.Event {
		raw := fmt.Sprintf(`{"version":4,"event_id":"b1e70001-%d","source":"wraith-nano","sensor_id":"nano-70b2a8","event_type":"%s","timestamp":0,"seq":%d,"metadata":%s}`,
			seq, typ, seq, meta)
		ev, err := events.Parse([]byte(raw))
		if err != nil {
			t.Fatalf("fixture invalid: %v\n%s", err, raw)
		}
		return ev
	}
	feed(t, p,
		v4(0, events.TypeBtSystemStatus, `{"state":"scanning_dual","mode":"bluetooth","classic_devices":0,"ble_devices":0}`),
		v4(1, events.TypeBtClassicDiscovered, `{"address":"AA:BB:CC:DD:EE:01","name":"Speaker","cod":2360324,"device_class":"audio","rssi":-62,"count":1}`),
		v4(2, events.TypeBtBleAdvObserved, `{"address":"11:22:33:44:55:66","address_type":"random","rssi":-70,"manufacturer_id":76,"manufacturer_data":"4c000215","count":4,"window_ms":10000}`),
		v4(3, events.TypeBtBleDiscovered, `{"address":"11:22:33:44:55:66","address_type":"random","rssi":-70,"count":1}`),
		v4(4, events.TypeBtSystemError, `{"code":"inquiry_failed","message":"esp_bt_gap_start_discovery: ESP_FAIL"}`),
	)
	if p.Failures.Load() != 0 || p.Ingested.Load() != 5 {
		t.Fatalf("failures=%d ingested=%d", p.Failures.Load(), p.Ingested.Load())
	}
	evs, total, err := st.ListEvents(ctx, storage.Page{Limit: 10}, storage.EventFilter{})
	if err != nil || total != 5 || len(evs) != 5 {
		t.Fatalf("stored events: %v total=%d", err, total)
	}
	if _, aps, _ := st.ListAccessPoints(ctx, storage.Page{Limit: 10}); aps != 0 {
		t.Fatalf("bluetooth events created %d AP rows", aps)
	}
	if _, fs, _ := st.ListFindings(ctx, storage.Page{Limit: 10}); fs != 0 {
		t.Fatalf("bluetooth events produced %d Wi-Fi findings", fs)
	}
	// 5 stored, 4 streamed raw: the advertisement report is pushed only as a
	// (throttled) bt_device update — see TestBluetoothStreamThrottled. The
	// classic + BLE device rows each produce one bt_device; status and error
	// each produce one bt_status. No Wi-Fi stream kinds.
	if pub.msgs[stream.KindEvent] != 4 || pub.msgs[stream.KindBTDevice] != 2 || pub.msgs[stream.KindBTStatus] != 2 ||
		pub.msgs[stream.KindAP] != 0 || pub.msgs[stream.KindClient] != 0 {
		t.Fatalf("published: %v", pub.msgs)
	}
	// The sensor is still tracked (seq continuity across the v4 events).
	s, err := st.GetSensor(ctx, "nano-70b2a8")
	if err != nil || s.GapCount != 0 {
		t.Fatalf("sensor: %+v %v", s, err)
	}
}

// bt builds and validates a v4 Bluetooth event exactly as it would arrive.
func bt(t *testing.T, seq int, typ, meta string) *events.Event {
	t.Helper()
	raw := fmt.Sprintf(`{"version":4,"event_id":"b7b70002-%d","source":"wraith-nano","sensor_id":"nano-70b2a8","event_type":"%s","timestamp":0,"seq":%d,"metadata":%s}`,
		seq, typ, seq, meta)
	ev, err := events.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("fixture invalid: %v\n%s", err, raw)
	}
	return ev
}

// The Bluetooth inventory end to end: every v4 type is handled, counts follow
// docs/bluetooth/event-protocol.md "Counting", GATT events without an address
// type are linked to the right device, and Wi-Fi state is never touched.
func TestBluetoothInventoryThroughPipeline(t *testing.T) {
	p, st, _ := setup(t)
	ctx := context.Background()
	const spk, band = "AA:BB:CC:DD:EE:01", "11:22:33:44:55:66"
	feed(t, p,
		bt(t, 0, events.TypeBtSystemStatus, `{"state":"scanning_dual","mode":"bluetooth"}`),
		bt(t, 1, events.TypeBtClassicDiscovered, `{"address":"`+spk+`","cod":2360324,"count":1}`),
		bt(t, 2, events.TypeBtClassicUpdated, `{"address":"`+spk+`","name":"Speaker","rssi":-62,"count":2}`),
		bt(t, 3, events.TypeBtBleAdvObserved, `{"address":"`+band+`","address_type":"random","rssi":-80,"services":["180F"],"manufacturer_id":343,"count":4}`),
		bt(t, 4, events.TypeBtBleAdvObserved, `{"address":"`+band+`","address_type":"random","rssi":-60,"services":["fe95"]}`),
		bt(t, 5, events.TypeBtBleDiscovered, `{"address":"`+band+`","address_type":"random","name":"Band","classification":"wearable","confidence":0.7}`),
		bt(t, 6, events.TypeBtBleUpdated, `{"address":"`+band+`","address_type":"random","count":2}`),
		bt(t, 7, events.TypeBtBleConnObserved, `{"address":"`+band+`","address_type":"random"}`),
		bt(t, 8, events.TypeBtGattService, `{"address":"`+band+`","uuid":"180f","primary":true}`),
		bt(t, 9, events.TypeBtGattChar, `{"address":"`+band+`","service_uuid":"180f","uuid":"2a19","properties":["read","notify"]}`),
		bt(t, 10, events.TypeBtBleConnLost, `{"address":"`+band+`","reason":"operator disconnect"}`),
		bt(t, 11, events.TypeBtSystemError, `{"code":"inquiry_failed","message":"ESP_FAIL"}`),
		bt(t, 12, events.TypeBtSystemError, `{"code":"scan_stalled","message":"no reports for 30 s"}`),
	)
	if p.Failures.Load() != 0 || p.Ingested.Load() != 13 {
		t.Fatalf("failures=%d ingested=%d", p.Failures.Load(), p.Ingested.Load())
	}

	c, err := st.GetBTDevice(ctx, storage.BTClassic, spk, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Observations != 3 || *c.Name != "Speaker" || *c.DeviceClass != "audio" || *c.CoD != 2360324 || *c.LastRSSI != -62 {
		t.Fatalf("classic: %+v", c)
	}

	b, err := st.GetBTDevice(ctx, storage.BTBLE, band, "random")
	if err != nil {
		t.Fatal(err)
	}
	// 4 + 1 (adv without count) + 0 (device record without count) + 2 (explicit)
	if b.Observations != 7 || *b.Name != "Band" || *b.Classification != "wearable" || *b.ManufacturerID != 343 {
		t.Fatalf("ble: %+v", b)
	}
	if len(b.Services) != 2 || b.Services[0] != "180f" || b.Services[1] != "fe95" || *b.RSSIAvg != -75 {
		t.Fatalf("ble services/rssi: %v avg=%v", b.Services, *b.RSSIAvg)
	}

	// adv×2 + discovered + updated + conn observed + gatt×2 + conn lost (resolved to "random").
	obs, err := st.ListBTObservations(ctx, storage.BTBLE, band, "random", 50)
	if err != nil || len(obs) != 8 {
		t.Fatalf("ble observations: %v %d", err, len(obs))
	}
	all, total, _ := st.ListBTDevices(ctx, storage.Page{Limit: 10}, storage.BTDeviceFilter{})
	if total != 2 || len(all) != 2 {
		t.Fatalf("devices: %d %+v", total, all)
	}
	if _, n, _ := st.ListBTDevices(ctx, storage.Page{Limit: 10}, storage.BTDeviceFilter{Transport: "classic"}); n != 1 {
		t.Fatalf("classic filter: %d", n)
	}

	ss, err := st.ListBTStatus(ctx)
	if err != nil || len(ss) != 1 {
		t.Fatalf("status: %v %+v", err, ss)
	}
	s := ss[0]
	if *s.Mode != "bluetooth" || *s.State != "scanning_dual" || s.ErrorCount != 2 || *s.LastErrorCode != "scan_stalled" {
		t.Fatalf("status row: %+v", s)
	}

	// Wi-Fi state untouched.
	if _, n, _ := st.ListAccessPoints(ctx, storage.Page{Limit: 10}); n != 0 {
		t.Fatalf("AP rows: %d", n)
	}
	if _, n, _ := st.ListClients(ctx, storage.Page{Limit: 10}, ""); n != 0 {
		t.Fatalf("client rows: %d", n)
	}
}

// Retention: Bluetooth addresses not seen within the window are pruned with
// their events (rotating BLE addresses must not grow the table forever);
// recently seen ones and their history stay.
func TestBluetoothRetention(t *testing.T) {
	p, st, _ := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now.AddDate(0, 0, -10) }
	feed(t, p, bt(t, 0, events.TypeBtBleAdvObserved, `{"address":"02:00:00:00:00:01","address_type":"random","rssi":-70}`))
	p.now = func() time.Time { return now }
	feed(t, p, bt(t, 1, events.TypeBtBleAdvObserved, `{"address":"02:00:00:00:00:02","address_type":"random","rssi":-71}`))

	res, err := st.PruneEvents(ctx, now.AddDate(0, 0, -7).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 1 || res.BTDevices != 1 {
		t.Fatalf("prune result: %+v", res)
	}
	if _, err := st.GetBTDevice(ctx, storage.BTBLE, "02:00:00:00:00:01", "random"); err != storage.ErrNotFound {
		t.Fatalf("old address should be pruned: %v", err)
	}
	if o, _ := st.ListBTObservations(ctx, storage.BTBLE, "02:00:00:00:00:01", "random", 10); len(o) != 0 {
		t.Fatalf("old observations should cascade away: %d", len(o))
	}
	if o, _ := st.ListBTObservations(ctx, storage.BTBLE, "02:00:00:00:00:02", "random", 10); len(o) != 1 {
		t.Fatalf("recent observations kept: %d", len(o))
	}
}

// Live stream: raw advertisement reports are not pushed (they arrive as
// throttled bt_device updates), status changes are, and the throttle drops
// repeated updates for one device within StreamPerDevice.
func TestBluetoothStreamThrottled(t *testing.T) {
	p, _, pub := setup(t)
	t0 := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return t0 }
	adv := func(seq int) *events.Event {
		return bt(t, seq, events.TypeBtBleAdvObserved, `{"address":"11:22:33:44:55:66","address_type":"random","rssi":-70}`)
	}
	feed(t, p, adv(0), adv(1), adv(2))
	if pub.msgs[stream.KindEvent] != 0 {
		t.Fatalf("advertisement reports must not be streamed raw: %v", pub.msgs)
	}
	if pub.msgs[stream.KindBTDevice] != 1 || p.BTStreamSuppressed() != 2 {
		t.Fatalf("throttle: %v suppressed=%d", pub.msgs, p.BTStreamSuppressed())
	}
	p.now = func() time.Time { return t0.Add(bluetooth.StreamPerDevice) }
	feed(t, p, adv(3))
	if pub.msgs[stream.KindBTDevice] != 2 {
		t.Fatalf("after interval: %v", pub.msgs)
	}
	// Device records and status are ordinary events; status goes out as bt_status.
	feed(t, p,
		bt(t, 4, events.TypeBtBleDiscovered, `{"address":"02:00:00:00:00:07","address_type":"public"}`),
		bt(t, 5, events.TypeBtSystemStatus, `{"state":"idle","mode":"wifi"}`))
	if pub.msgs[stream.KindEvent] != 2 || pub.msgs[stream.KindBTDevice] != 3 || pub.msgs[stream.KindBTStatus] != 1 {
		t.Fatalf("published: %v", pub.msgs)
	}
	if s := pub.last[stream.KindBTStatus].(storage.BTSensorStatus); *s.Mode != "wifi" {
		t.Fatalf("bt_status payload: %+v", s)
	}
}
