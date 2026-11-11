package ingest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
