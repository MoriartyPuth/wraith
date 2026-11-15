package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func memStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func exec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

var evSeq int

func addEvent(t *testing.T, s *Store, sensor, typ string, at int64, meta string) {
	t.Helper()
	evSeq++
	payload := fmt.Sprintf(`{"version":4,"event_id":"x-%d","event_type":%q,"metadata":%s}`, evSeq, typ, meta)
	exec(t, s, `INSERT INTO events(event_id, sensor_id, source, event_type, sensor_ts, received_at, payload)
		VALUES (?, ?, 'wraith-nano', ?, 0, ?, ?)`, fmt.Sprintf("x-%d", evSeq), sensor, typ, at, payload)
}

func TestRadioStatesAndHeartbeatGaps(t *testing.T) {
	s := memStore(t)
	ctx := context.Background()
	addEvent(t, s, "n1", "bluetooth.system.status", 50, `{"mode":"bluetooth","state":"scanning_dual"}`)
	addEvent(t, s, "n1", "bluetooth.system.status", 150, `{"mode":"wifi","state":"idle"}`)
	addEvent(t, s, "n2", "bluetooth.system.status", 160, `{"mode":"bluetooth","state":"scanning_ble"}`) // other sensor
	st, ok, err := s.RadioStateBefore(ctx, "n1", 100)
	if err != nil || !ok || st != (RadioState{At: 50, Mode: "bluetooth", State: "scanning_dual"}) {
		t.Fatalf("before: %+v %v %v", st, ok, err)
	}
	if _, ok, err := s.RadioStateBefore(ctx, "n1", 50); ok || err != nil {
		t.Fatal("nothing before the first report")
	}
	sts, err := s.RadioStates(ctx, "n1", 100, 1000)
	if err != nil || len(sts) != 1 || sts[0].Mode != "wifi" {
		t.Fatalf("states: %+v %v", sts, err)
	}

	for _, at := range []int64{1000, 6000, 11000, 41000, 46000} { // 30 s silence after 11000
		addEvent(t, s, "n1", "sensor.heartbeat", at, `{}`)
	}
	addEvent(t, s, "n1", "wifi.ap_observed", 25000, `{}`) // not a heartbeat
	gaps, err := s.HeartbeatGaps(ctx, "n1", 0, 100000, 20000)
	want := [][2]int64{{11000, 41000}, {46000, 100000}}
	if err != nil || !reflect.DeepEqual(gaps, want) {
		t.Fatalf("gaps %v %v", gaps, err)
	}
	// Leading silence longer than the threshold counts; shorter does not.
	if g, _ := s.HeartbeatGaps(ctx, "n1", -30000, 47000, 20000); !reflect.DeepEqual(g, [][2]int64{{-30000, 1000}, {11000, 41000}}) {
		t.Fatalf("leading: %v", g)
	}
	if g, _ := s.HeartbeatGaps(ctx, "nobody", 0, 100000, 20000); !reflect.DeepEqual(g, [][2]int64{{0, 100000}}) {
		t.Fatalf("no heartbeats: %v", g)
	}
}

func TestSearch(t *testing.T) {
	s := memStore(t)
	ctx := context.Background()
	exec(t, s, `INSERT INTO access_points(bssid, ssid, ssid_hex, first_seen, last_seen, auth_mode) VALUES
		('F8:CE:21:FD:C4:EC', 'Rozas-001', '', 1, 10, 'wpa-wpa2-psk'),
		('64:13:AB:A5:53:E8', '', '', 1, 20, 'wpa2-psk'),
		('AA:00:00:00:00:01', '50%_off', '', 1, 5, NULL)`)
	exec(t, s, `INSERT INTO clients(bssid, client, first_seen, last_seen) VALUES
		('F8:CE:21:FD:C4:EC', '32:B4:EC:04:82:C5', 1, 30),
		('64:13:AB:A5:53:E8', '32:B4:EC:04:82:C5', 1, 40)`)
	exec(t, s, `INSERT INTO bt_devices(transport, address, address_type, name, device_class, first_seen, last_seen) VALUES
		('classic', 'E4:0C:FD:77:F9:84', '', 'OPPO A54', 'phone', 1, 50),
		('ble', 'C3:11:22:33:44:55', 'random', NULL, NULL, 1, 60)`)
	find := func(q string) []string {
		t.Helper()
		hits, err := s.Search(ctx, q, 8)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hits {
			out = append(out, h.Kind+"|"+h.ID+"|"+h.Title+"|"+h.Detail)
		}
		return out
	}
	eq := func(q string, want ...string) {
		t.Helper()
		if got := find(q); !reflect.DeepEqual(got, want) {
			t.Errorf("search %q:\n got  %q\n want %q", q, got, want)
		}
	}
	eq("rozas", "access_point|F8:CE:21:FD:C4:EC|Rozas-001|F8:CE:21:FD:C4:EC · wpa-wpa2-psk")
	eq("oppo", "bt_device|classic:E4:0C:FD:77:F9:84|OPPO A54|E4:0C:FD:77:F9:84 · Classic · phone")
	// Addresses: any separator or none, partial, case-insensitive.
	eq("53:e8", "access_point|64:13:AB:A5:53:E8|(hidden SSID)|64:13:AB:A5:53:E8 · wpa2-psk")
	eq("32b4ec", "client|32:B4:EC:04:82:C5|32:B4:EC:04:82:C5|client · last with 64:13:AB:A5:53:E8 · 2 APs")
	eq("c3-11-22", "bt_device|ble:random:C3:11:22:33:44:55|C3:11:22:33:44:55|C3:11:22:33:44:55 · BLE random")
	// "ec" is both text and hex: SSIDs, BSSIDs, clients, BT addresses.
	if got := find("ec"); len(got) != 2 || got[0] != "access_point|F8:CE:21:FD:C4:EC|Rozas-001|F8:CE:21:FD:C4:EC · wpa-wpa2-psk" {
		t.Errorf("ec: %q", got)
	}
	// LIKE wildcards in the query are literal.
	eq("50%", "access_point|AA:00:00:00:00:01|50%_off|AA:00:00:00:00:01")
	eq("0%_", "access_point|AA:00:00:00:00:01|50%_off|AA:00:00:00:00:01")
	eq("%%")
	eq("zz-not-there")
	// Per-kind limit.
	for i := 0; i < 12; i++ {
		exec(t, s, `INSERT INTO bt_devices(transport, address, address_type, name, first_seen, last_seen) VALUES ('ble', ?, 'random', 'Band', 1, ?)`,
			fmt.Sprintf("D0:00:00:00:00:%02X", i), 100+i)
	}
	if got := find("band"); len(got) != 8 || got[0] != "bt_device|ble:random:D0:00:00:00:00:0B|Band|D0:00:00:00:00:0B · BLE random" {
		t.Errorf("limit/order: %d %q", len(got), got)
	}
}
