package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

func TestClientsEndpoints(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	const bssid = "64:13:AB:A5:53:E8"
	const phone = "F6:C1:32:28:B8:85"
	const laptop = "32:B4:EC:04:82:C5"
	now := time.Now().UnixMilli()
	ssid := "Bo"
	ch := 11
	err := st.WithTx(context.Background(), func(tx *storage.Tx) error {
		if err := tx.UpsertAP(context.Background(),
			storage.APUpdate{BSSID: bssid, SSID: &ssid, Seen: now, Channel: &ch, Sensor: "nano-1"}); err != nil {
			return err
		}
		for _, c := range []storage.ClientUpdate{
			{BSSID: bssid, Client: phone, Seen: now, Frames: 12, Channel: &ch, RSSI: intp(-55), Sensor: "nano-1"},
			{BSSID: bssid, Client: laptop, Seen: now - 1000, Frames: 3, Channel: &ch, RSSI: intp(-70), Sensor: "nano-1"},
		} {
			if err := tx.UpsertClient(context.Background(), c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// All clients, newest first, SSID joined from the AP inventory.
	code, body := get(t, srv, "/api/v1/clients?limit=10")
	if code != http.StatusOK {
		t.Fatalf("clients: %d %v", code, body)
	}
	rows := body["data"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows: %v", body)
	}
	first := rows[0].(map[string]any)
	if first["client"] != phone || first["ssid"] != "Bo" || first["frames"].(float64) != 12 {
		t.Fatalf("first row: %v", first)
	}

	// Per-AP (plain and %3A-encoded path).
	for _, path := range []string{
		"/api/v1/access-points/" + bssid + "/clients",
		"/api/v1/access-points/64%3A13%3AAB%3AA5%3A53%3AE8/clients",
	} {
		code, body = get(t, srv, path)
		if code != http.StatusOK {
			t.Fatalf("%s: %d %v", path, code, body)
		}
		if n := len(body["data"].([]any)); n != 2 {
			t.Fatalf("%s rows: %d", path, n)
		}
	}

	// Filter query and invalid filter.
	if code, body = get(t, srv, "/api/v1/clients?bssid="+bssid); code != http.StatusOK {
		t.Fatalf("bssid filter: %d %v", code, body)
	}
	if code, _ = get(t, srv, "/api/v1/clients?bssid=nope"); code != http.StatusBadRequest {
		t.Fatalf("bad bssid filter: %d", code)
	}
	// Unknown-but-valid AP has no sightings.
	if code, body = get(t, srv, "/api/v1/access-points/AA:AA:AA:AA:AA:AA/clients"); code != http.StatusOK {
		t.Fatalf("empty ap clients: %d %v", code, body)
	}
}

func intp(v int) *int { return &v }

// TestClientCounts covers the client counts on the AP list/detail and
// /statistics: per-AP total vs recent (10-min window), APs without clients
// report 0, and a station seen on two APs counts once in the statistics.
func TestClientCounts(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	ctx := context.Background()
	const apA, apB = "64:13:AB:A5:53:E8", "FC:12:2C:33:6A:58"
	const apEmpty = "AA:BB:CC:DD:EE:01"
	now := time.Now().UnixMilli()
	old := now - int64(time.Hour/time.Millisecond)
	err := st.WithTx(ctx, func(tx *storage.Tx) error {
		for _, b := range []string{apA, apB, apEmpty} {
			if err := tx.UpsertAP(ctx, storage.APUpdate{BSSID: b, Seen: now, Sensor: "nano-1"}); err != nil {
				return err
			}
		}
		for _, c := range []storage.ClientUpdate{
			{BSSID: apA, Client: "11:11:11:11:11:01", Seen: now, Frames: 1, Sensor: "nano-1"},
			{BSSID: apA, Client: "11:11:11:11:11:02", Seen: old, Frames: 1, Sensor: "nano-1"},
			{BSSID: apA, Client: "11:11:11:11:11:03", Seen: old, Frames: 1, Sensor: "nano-1"},
			// Same station also on apB (roamed): one distinct client overall.
			{BSSID: apB, Client: "11:11:11:11:11:01", Seen: now, Frames: 1, Sensor: "nano-1"},
		} {
			if err := tx.UpsertClient(ctx, c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][2]float64{apA: {3, 1}, apB: {1, 1}, apEmpty: {0, 0}}
	code, body := get(t, srv, "/api/v1/access-points?limit=10")
	if code != http.StatusOK {
		t.Fatalf("aps: %d %v", code, body)
	}
	rows := body["data"].([]any)
	if len(rows) != 3 {
		t.Fatalf("ap rows: %v", rows)
	}
	for _, r := range rows {
		a := r.(map[string]any)
		w := want[a["bssid"].(string)]
		if a["clients"] != w[0] || a["recent_clients"] != w[1] {
			t.Fatalf("%s: clients=%v recent=%v, want %v", a["bssid"], a["clients"], a["recent_clients"], w)
		}
	}

	code, body = get(t, srv, "/api/v1/access-points/"+apA)
	if code != http.StatusOK {
		t.Fatalf("ap: %d %v", code, body)
	}
	if a := body["data"].(map[string]any); a["clients"] != 3.0 || a["recent_clients"] != 1.0 {
		t.Fatalf("ap detail counts: %v", a)
	}

	code, body = get(t, srv, "/api/v1/statistics")
	if code != http.StatusOK {
		t.Fatalf("stats: %d %v", code, body)
	}
	if body["clients"] != 3.0 || body["recent_clients"] != 1.0 || body["client_window_ms"] != 600000.0 {
		t.Fatalf("stats counts: clients=%v recent=%v window=%v", body["clients"], body["recent_clients"], body["client_window_ms"])
	}
}
