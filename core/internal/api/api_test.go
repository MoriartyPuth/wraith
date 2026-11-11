package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/system"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := NewRouter(Deps{
		Store:   st,
		Capture: capture.NewManager(capture.NewStub("fake", false, "test")),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Started: time.Now(),
		Interfaces: func() ([]system.WirelessInterface, error) {
			return []system.WirelessInterface{{Name: "wlan0", MonitorCapable: true}}, nil
		},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s: non-JSON body: %v", path, err)
	}
	return resp.StatusCode, body
}

func TestEndpoints(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		path string
		code int
	}{
		{"/api/v1/health", 200},
		{"/api/v1/system/info", 200},
		{"/api/v1/sensors", 200},
		{"/api/v1/sensors/nano-001", 404},
		{"/api/v1/sensors/bad%20id", 400},
		{"/api/v1/access-points", 200},
		{"/api/v1/access-points/aa:bb:cc:dd:ee:ff", 404},
		{"/api/v1/access-points/zz", 400},
		{"/api/v1/events", 200},
		{"/api/v1/events/1", 404},
		{"/api/v1/events/abc", 400},
		{"/api/v1/detections", 200},
		{"/api/v1/detections/f-1", 404},
		{"/api/v1/capture/status", 200},
		{"/api/v1/capture/interfaces", 200},
		{"/api/v1/statistics", 200},
		{"/api/v1/ws", 503},
		{"/api/v1/access-points/AA:BB:CC:DD:EE:FF/observations", 200},
		{"/api/v1/access-points/zz/observations", 400},
		{"/api/v1/events?type=sensor.heartbeat,wifi.ap_observed&bssid=aa:bb:cc:dd:ee:ff&sensor=nano-1&since=1&until=99999999999999", 200},
		{"/api/v1/events?bssid=nope", 400},
		{"/api/v1/events?event_id=a-1,b-2", 200},
		{"/api/v1/events?event_id=bad%20id", 400},
		{"/api/v1/events?since=-5", 400},
		{"/api/v1/events?sensor=bad%20id", 400},
		{"/api/v1/nope", 404},
		{"/api/v1/events?limit=0", 400},
		{"/api/v1/events?limit=501", 400},
		{"/api/v1/events?offset=-1", 400},
	}
	for _, c := range cases {
		code, body := get(t, srv, c.path)
		if code != c.code {
			t.Errorf("%s: status %d, want %d (%v)", c.path, code, c.code, body)
		}
		if code >= 400 {
			e, ok := body["error"].(map[string]any)
			if !ok || e["code"] == "" || e["message"] == "" {
				t.Errorf("%s: inconsistent error body %v", c.path, body)
			}
		}
	}
}

func TestListShape(t *testing.T) {
	srv := newTestServer(t)
	_, body := get(t, srv, "/api/v1/events?limit=10&offset=0")
	data, ok := body["data"].([]any)
	if !ok || len(data) != 0 {
		t.Fatalf("data should be an empty array, got %v", body["data"])
	}
	p := body["pagination"].(map[string]any)
	if p["limit"].(float64) != 10 || p["total"].(float64) != 0 {
		t.Fatalf("pagination = %v", p)
	}
}

func TestHostGuardBlocksRebinding(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/health", nil)
	req.Host = "attacker.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host accepted: %d", resp.StatusCode)
	}
}

func newStoreServer(t *testing.T) (*httptest.Server, *storage.Store, *time.Time) {
	t.Helper()
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	h := NewRouter(Deps{Store: st, Capture: capture.NewManager(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Started: now, Now: func() time.Time { return now },
		Interfaces: func() ([]system.WirelessInterface, error) { return nil, nil }})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, st, &now
}

func TestPatchFinding(t *testing.T) {
	srv, st, _ := newStoreServer(t)
	ctx := context.Background()
	err := st.WithTx(ctx, func(tx *storage.Tx) error {
		return tx.InsertFinding(ctx, storage.Finding{ID: "f-1", Category: "c", Severity: "low", Confidence: 0.5,
			Classification: "suspicious", Explanation: "e", Evidence: []string{"a-1"}, CreatedAt: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	do := func(ct, body string) int {
		req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/detections/f-1", strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// A cross-site HTML form can only send these content types: rejected.
	if c := do("application/x-www-form-urlencoded", `{"status":"reviewed"}`); c != 415 {
		t.Fatalf("form content type: %d", c)
	}
	if c := do("text/plain", `{"status":"reviewed"}`); c != 415 {
		t.Fatalf("text/plain: %d", c)
	}
	if c := do("application/json", `{"status":"confirmed"}`); c != 400 {
		t.Fatalf("analyst-only value via API must be limited: %d", c)
	}
	if c := do("application/json", `{"status":"reviewed","x":1}`); c != 400 {
		t.Fatalf("unknown field: %d", c)
	}
	if c := do("application/json", `{"status":"reviewed"}`); c != 200 {
		t.Fatalf("valid patch: %d", c)
	}
	f, _ := st.GetFinding(ctx, "f-1")
	if f.Status != "reviewed" || f.Evidence[0] != "a-1" {
		t.Fatalf("finding: %+v", f)
	}
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/detections/nope", strings.NewReader(`{"status":"open"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing finding: %d", resp.StatusCode)
	}
}

func TestSensorConnectedFlag(t *testing.T) {
	srv, st, now := newStoreServer(t)
	ctx := context.Background()
	st.WithTx(ctx, func(tx *storage.Tx) error {
		tx.TouchSensor(ctx, "fresh", "wraith-nano", now.UnixMilli())
		tx.SensorHeartbeat(ctx, "fresh", now.Add(-5*time.Second).UnixMilli(), nil, "b")
		tx.TouchSensor(ctx, "stale", "wraith-nano", now.UnixMilli())
		tx.SensorHeartbeat(ctx, "stale", now.Add(-time.Minute).UnixMilli(), nil, "b")
		return nil
	})
	_, body := get(t, srv, "/api/v1/sensors")
	got := map[string]bool{}
	for _, x := range body["data"].([]any) {
		m := x.(map[string]any)
		got[m["id"].(string)] = m["connected"].(bool)
	}
	if !got["fresh"] || got["stale"] {
		t.Fatalf("connected flags: %v", got)
	}
}
