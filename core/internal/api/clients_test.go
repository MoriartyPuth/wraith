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
