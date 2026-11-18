package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestWatchlistEndpoints(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	seedBluetooth(t, st)
	bandID := "ble:random:" + btBand
	spkID := "classic:" + btSpk

	do := func(method, path, ct, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	rows := func(path string) []any {
		t.Helper()
		code, body := get(t, srv, path)
		if code != http.StatusOK {
			t.Fatalf("%s: %d %v", path, code, body)
		}
		return body["data"].([]any)
	}

	// Content-type gate on the write.
	if code, _ := do("PUT", "/api/v1/bluetooth/watchlist/"+bandID, "text/plain", `{"label":"x"}`); code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type: %d", code)
	}
	// Unknown device.
	if code, _ := do("PUT", "/api/v1/bluetooth/watchlist/ble:public:AA:AA:AA:AA:AA:AA", "application/json", `{}`); code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", code)
	}

	// Add (encoded id path: colons as %3A).
	code, body := do("PUT", "/api/v1/bluetooth/watchlist/ble%3Arandom%3A11%3A22%3A33%3A44%3A55%3A66",
		"application/json", `{"label":"My band"}`)
	if code != http.StatusOK {
		t.Fatalf("put: %d %v", code, body)
	}
	w := body["data"].(map[string]any)
	if w["label"] != "My band" || w["present"] != true || w["id"] != bandID {
		t.Fatalf("put row: %v", w)
	}

	// List: one entry, with 24 h presence computed from sightings.
	ws := rows("/api/v1/bluetooth/watchlist")
	if len(ws) != 1 {
		t.Fatalf("watchlist: %v", ws)
	}
	v := ws[0].(map[string]any)
	if v["id"] != bandID || v["sessions_24h"].(float64) < 1 || v["device"] == nil {
		t.Fatalf("watch row: %v", v)
	}

	// Sessions for the watch (plain and encoded id).
	for _, id := range []string{bandID, "ble%3Arandom%3A11%3A22%3A33%3A44%3A55%3A66"} {
		code, body = get(t, srv, "/api/v1/bluetooth/watchlist/"+id+"/sessions")
		if code != http.StatusOK {
			t.Fatalf("sessions %s: %d %v", id, code, body)
		}
		d := body["data"].(map[string]any)
		if d["present"] != true || len(d["sessions"].([]any)) < 1 {
			t.Fatalf("sessions data: %v", d)
		}
	}
	// Sessions for any observed device (not watched) are allowed.
	if code, _ := get(t, srv, "/api/v1/bluetooth/devices/"+spkID+"/sessions"); code != http.StatusOK {
		t.Fatalf("device sessions: %d", code)
	}
	// Watch sessions for a device that is not watched: 404.
	if code, _ := get(t, srv, "/api/v1/bluetooth/watchlist/"+spkID+"/sessions"); code != http.StatusNotFound {
		t.Fatalf("unwatched sessions: %d", code)
	}

	// Remove.
	if code, _ := do("DELETE", "/api/v1/bluetooth/watchlist/"+bandID, "", ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if len(rows("/api/v1/bluetooth/watchlist")) != 0 {
		t.Fatal("watchlist not empty after delete")
	}
	if code, _ := do("DELETE", "/api/v1/bluetooth/watchlist/"+bandID, "", ""); code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", code)
	}
}
