package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/btschedule"
	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/system"
)

func getJSON(t *testing.T, u string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestCoverageEndpoint(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	seq := 0
	add := func(typ string, at int64, meta string) {
		seq++
		raw := fmt.Sprintf(`{"version":4,"event_id":"c-%d","event_type":%q,"metadata":%s}`, seq, typ, meta)
		if err := st.WithTx(ctx, func(tx *storage.Tx) error {
			_, _, err := tx.InsertEvent(ctx, storage.StoredEvent{EventID: fmt.Sprintf("c-%d", seq), SensorID: "nano-1",
				Source: "wraith-nano", EventType: typ, ReceivedAt: at, Payload: json.RawMessage(raw)})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.WithTx(ctx, func(tx *storage.Tx) error {
		return tx.TouchSensor(ctx, "nano-1", "wraith-nano", now)
	}); err != nil {
		t.Fatal(err)
	}
	since := now - 120_000
	for at := now - 60_000; at < now; at += 5000 {
		add("sensor.heartbeat", at, `{}`)
	}
	add("bluetooth.system.status", now-30_000, `{"mode":"bluetooth","state":"scanning_dual"}`)

	code, body := getJSON(t, fmt.Sprintf("%s/api/v1/coverage?since=%d&until=%d", srv.URL, since, now+3_600_000))
	if code != 200 {
		t.Fatalf("coverage: %d %v", code, body)
	}
	d := body["data"].(map[string]any)
	if int64(d["until"].(float64)) > time.Now().UnixMilli() {
		t.Fatal("until must be clamped to now")
	}
	sensors := d["sensors"].([]any)
	if len(sensors) != 1 {
		t.Fatalf("sensors: %v", sensors)
	}
	segs := sensors[0].(map[string]any)["segments"].([]any)
	var kinds []string
	for _, s := range segs {
		m := s.(map[string]any)
		k := m["kind"].(string)
		if m["classic"] == true {
			k += "+classic"
		}
		kinds = append(kinds, k)
	}
	want := []string{"no_data", "wifi", "bluetooth+classic"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("segments %v want %v (%v)", kinds, want, segs)
	}

	for _, q := range []string{"since=abc", "since=-5", fmt.Sprintf("since=%d&until=%d", now, now-1),
		fmt.Sprintf("since=%d&until=%d", now-8*24*3_600_000, now)} {
		if code, _ := getJSON(t, srv.URL+"/api/v1/coverage?"+q); code != 400 {
			t.Errorf("%s: %d, want 400", q, code)
		}
	}
	// Default window works on any store.
	if code, _ := getJSON(t, srv.URL+"/api/v1/coverage"); code != 200 {
		t.Fatalf("default window: %d", code)
	}
}

func TestSearchEndpoint(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	upsertAP(t, st, "AA:BB:CC:DD:EE:01", 6)
	for _, q := range []string{"", "a", " a ", string(make([]byte, 65))} {
		if code, _ := getJSON(t, srv.URL+"/api/v1/search?q="+url.QueryEscape(q)); code != 400 {
			t.Errorf("q=%q: %d, want 400", q, code)
		}
	}
	code, body := getJSON(t, srv.URL+"/api/v1/search?q="+url.QueryEscape("ee:01"))
	hits, _ := body["data"].([]any)
	if code != 200 || len(hits) != 1 || hits[0].(map[string]any)["kind"] != "access_point" {
		t.Fatalf("search: %d %v", code, body)
	}
	code, body = getJSON(t, srv.URL+"/api/v1/search?q=nothing-here")
	if hits, ok := body["data"].([]any); code != 200 || !ok || len(hits) != 0 {
		t.Fatalf("empty search must return []: %d %v", code, body)
	}
}

// fakeScheduler is an in-memory Scheduler for the handler tests.
type fakeScheduler struct {
	cfg       btschedule.Schedule
	inSession bool
	setErr    error
}

func (f *fakeScheduler) Get() (btschedule.Schedule, int64, bool, bool) {
	var next int64
	ok := false
	if f.cfg.Enabled {
		next, ok = f.cfg.NextStart(1_000_000), true
	}
	return f.cfg, next, ok, f.inSession
}
func (f *fakeScheduler) Set(_ context.Context, cfg btschedule.Schedule) error {
	if f.setErr != nil {
		return f.setErr
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	f.cfg = cfg
	return nil
}

func newScheduleServer(t *testing.T, sch Scheduler) *httptest.Server {
	t.Helper()
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := NewRouter(Deps{Store: st, Capture: capture.NewManager(newFakeCommander()),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Started: time.Now(), Scheduler: sch,
		Interfaces: func() ([]system.WirelessInterface, error) { return nil, nil }})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func putJSON(t *testing.T, u, ctype, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("PUT", u, strings.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestBTScheduleEndpoint(t *testing.T) {
	srv := newScheduleServer(t, &fakeScheduler{cfg: btschedule.Default()})
	const js = "application/json"

	code, body := getJSON(t, srv.URL+"/api/v1/bluetooth/schedule")
	d, _ := body["data"].(map[string]any)
	if code != 200 || d["in_session"] != false || d["schedule"].(map[string]any)["enabled"] != false {
		t.Fatalf("get default: %d %v", code, body)
	}
	if _, hasNext := d["next_run"]; hasNext {
		t.Fatal("disabled schedule should report no next_run")
	}
	if d["limits"].(map[string]any)["max_duration_seconds"].(float64) != float64(btschedule.MaxDurationSeconds) {
		t.Fatalf("limits missing: %v", d["limits"])
	}

	// CSRF guard: non-JSON content type is refused before anything changes.
	if c, _ := putJSON(t, srv.URL+"/api/v1/bluetooth/schedule", "text/plain", `{"enabled":true,"every_minutes":60,"duration_seconds":300,"classic":false}`); c != 415 {
		t.Fatalf("csrf guard: %d", c)
	}
	// Unknown field and out-of-range values are 400.
	for _, b := range []string{
		`{"enabled":true,"every_minutes":60,"duration_seconds":300,"classic":false,"x":1}`,
		`{"enabled":true,"every_minutes":5,"duration_seconds":300,"classic":false}`,
		`{"enabled":true,"every_minutes":60,"duration_seconds":5,"classic":false}`,
		`not json`,
	} {
		if c, _ := putJSON(t, srv.URL+"/api/v1/bluetooth/schedule", js, b); c != 400 {
			t.Errorf("want 400 for %s, got %d", b, c)
		}
	}
	// A valid PUT stores and echoes it, now with next_run.
	code, body = putJSON(t, srv.URL+"/api/v1/bluetooth/schedule", js, `{"enabled":true,"every_minutes":30,"duration_seconds":120,"classic":true}`)
	d, _ = body["data"].(map[string]any)
	if code != 200 || d["schedule"].(map[string]any)["classic"] != true || d["next_run"] == nil {
		t.Fatalf("valid put: %d %v", code, body)
	}
}

func TestBTScheduleUnavailable(t *testing.T) {
	srv := newScheduleServer(t, nil) // no scheduler wired
	if c, _ := getJSON(t, srv.URL+"/api/v1/bluetooth/schedule"); c != 503 {
		t.Fatalf("want 503 without scheduler, got %d", c)
	}
}
