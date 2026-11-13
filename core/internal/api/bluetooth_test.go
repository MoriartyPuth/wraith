package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/detection"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/ingest"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

const (
	btSpk  = "AA:BB:CC:DD:EE:01"
	btBand = "11:22:33:44:55:66"
)

// seedBluetooth feeds events through the real ingest pipeline, exactly as
// they would arrive from a sensor.
func seedBluetooth(t *testing.T, st *storage.Store) {
	t.Helper()
	p := ingest.New(st, detection.NewEngine(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	evs := []struct {
		v                int
		typ, extra, meta string
	}{
		{3, "wifi.ap_observed", `,"channel":6,"bssid":"64:13:AB:A5:53:E8"`, `{"ssid":"Lab","ssid_hex":"4c6162","rssi":-50}`},
		{4, events.TypeBtSystemStatus, "", `{"state":"scanning_dual","mode":"bluetooth","classic_devices":1,"ble_devices":1}`},
		{4, events.TypeBtClassicDiscovered, "", `{"address":"` + btSpk + `","name":"Speaker","cod":2360324,"rssi":-62}`},
		{4, events.TypeBtBleAdvObserved, "", `{"address":"` + btBand + `","address_type":"random","rssi":-70,"count":3}`},
		{4, events.TypeBtBleAdvObserved, "", `{"address":"` + btBand + `","address_type":"random","rssi":-66}`},
		{4, events.TypeBtGattService, "", `{"address":"` + btBand + `","uuid":"180f","start_handle":1,"end_handle":9,"primary":true}`},
		{4, events.TypeBtGattChar, "", `{"address":"` + btBand + `","service_uuid":"180f","uuid":"2a19","handle":3,"properties":["read"]}`},
		{4, events.TypeBtGattChar, "", `{"address":"` + btBand + `","service_uuid":"180f","uuid":"2a19","handle":3,"properties":["read","notify"]}`},
		{4, events.TypeBtSystemError, "", `{"code":"inquiry_failed","message":"ESP_FAIL"}`},
	}
	for i, e := range evs {
		raw := fmt.Sprintf(`{"version":%d,"event_id":"a1b2c3d4-%d","source":"wraith-nano","sensor_id":"nano-70b2a8","event_type":"%s","timestamp":0,"seq":%d%s,"metadata":%s}`,
			e.v, i, e.typ, i, e.extra, e.meta)
		ev, err := events.Parse([]byte(raw))
		if err != nil {
			t.Fatalf("seed %d invalid: %v", i, err)
		}
		if err := p.Handle(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBluetoothEndpoints(t *testing.T) {
	srv, st := newTxServer(t, false, nil)
	seedBluetooth(t, st)
	spkID := "classic:" + btSpk
	bandID := "ble:random:" + btBand

	data := func(path string, wantCode int) any {
		t.Helper()
		code, body := get(t, srv, path)
		if code != wantCode {
			t.Fatalf("%s: code %d, want %d: %v", path, code, wantCode, body)
		}
		return body["data"]
	}
	rows := func(path string) []any {
		t.Helper()
		return data(path, 200).([]any)
	}

	// Status: per-sensor mode/state/error tally and device counts.
	st0 := data("/api/v1/bluetooth/status", 200).(map[string]any)
	sensors := st0["sensors"].([]any)
	s := sensors[0].(map[string]any)
	if len(sensors) != 1 || s["mode"] != "bluetooth" || s["error_count"].(float64) != 1 {
		t.Fatalf("status sensors: %v", sensors)
	}
	dev := st0["devices"].(map[string]any)
	if dev["classic"].(float64) != 1 || dev["ble"].(float64) != 1 || dev["total"].(float64) != 2 || dev["recent_ble"].(float64) != 1 {
		t.Fatalf("status devices: %v", dev)
	}

	// Lists and filters.
	if n := len(rows("/api/v1/bluetooth/devices")); n != 2 {
		t.Fatalf("devices: %d", n)
	}
	if r := rows("/api/v1/bluetooth/devices?transport=ble"); len(r) != 1 || r[0].(map[string]any)["id"] != bandID {
		t.Fatalf("ble filter: %v", r)
	}
	if r := rows("/api/v1/bluetooth/classic/devices"); len(r) != 1 || r[0].(map[string]any)["id"] != spkID {
		t.Fatalf("classic list: %v", r)
	}
	if n := len(rows("/api/v1/bluetooth/ble/devices")); n != 1 {
		t.Fatalf("ble list: %d", n)
	}
	if n := len(rows("/api/v1/bluetooth/devices?since=99999999999999")); n != 0 {
		t.Fatalf("since filter: %d", n)
	}
	data("/api/v1/bluetooth/devices?transport=wifi", 400)
	data("/api/v1/bluetooth/classic/devices?transport=ble", 400)
	data("/api/v1/bluetooth/devices?since=-1", 400)

	// Device detail: plain, %3A-encoded, lowercase; bad and unknown ids.
	for _, path := range []string{
		"/api/v1/bluetooth/devices/" + spkID,
		"/api/v1/bluetooth/devices/classic%3AAA%3ABB%3ACC%3ADD%3AEE%3A01",
		"/api/v1/bluetooth/devices/classic:aa:bb:cc:dd:ee:01",
	} {
		d := data(path, 200).(map[string]any)
		if d["device_class"] != "audio" || d["name"] != "Speaker" || d["observations"].(float64) != 1 {
			t.Fatalf("%s: %v", path, d)
		}
	}
	data("/api/v1/bluetooth/devices/ble:static:"+btBand, 400)
	data("/api/v1/bluetooth/devices/classic:ZZ:BB:CC:DD:EE:01", 400)
	data("/api/v1/bluetooth/devices/classic:02:00:00:00:00:99", 404)
	data("/api/v1/bluetooth/devices/ble:public:"+btBand, 404) // same address, other type: not merged

	band := data("/api/v1/bluetooth/devices/"+bandID, 200).(map[string]any)
	if band["observations"].(float64) != 4 || band["last_rssi"].(float64) != -66 {
		t.Fatalf("ble device: %v", band)
	}

	// RSSI history and device events.
	if n := len(rows("/api/v1/bluetooth/devices/" + bandID + "/observations")); n != 5 { // 2 adv + 1 svc + 2 char
		t.Fatalf("observations: %d", n)
	}
	if n := len(rows("/api/v1/bluetooth/devices/" + bandID + "/observations?limit=1")); n != 1 {
		t.Fatalf("observations limit: %d", n)
	}
	data("/api/v1/bluetooth/devices/"+bandID+"/observations?limit=0", 400)
	data("/api/v1/bluetooth/devices/classic:02:00:00:00:00:99/observations", 404)
	code, body := get(t, srv, "/api/v1/bluetooth/devices/"+bandID+"/events?limit=2")
	if code != 200 || len(body["data"].([]any)) != 2 || body["pagination"].(map[string]any)["total"].(float64) != 5 {
		t.Fatalf("device events: %d %v", code, body)
	}

	// GATT: services, deduplicated characteristics (newest wins), BLE only.
	svcs := rows("/api/v1/bluetooth/ble/devices/" + bandID + "/services")
	if len(svcs) != 1 || svcs[0].(map[string]any)["uuid"] != "180f" {
		t.Fatalf("services: %v", svcs)
	}
	chars := rows("/api/v1/bluetooth/ble/devices/" + bandID + "/characteristics")
	if len(chars) != 1 || len(chars[0].(map[string]any)["properties"].([]any)) != 2 {
		t.Fatalf("characteristics (want newest of the duplicate): %v", chars)
	}
	if _, ok := chars[0].(map[string]any)["address"]; ok {
		t.Fatalf("address should not be repeated per item: %v", chars[0])
	}
	data("/api/v1/bluetooth/ble/devices/"+spkID+"/services", 400)

	// Bluetooth event history: bluetooth.* only (the Wi-Fi event is excluded).
	code, body = get(t, srv, "/api/v1/bluetooth/events?limit=100")
	if code != 200 || body["pagination"].(map[string]any)["total"].(float64) != 8 {
		t.Fatalf("bluetooth events: %d %v", code, body["pagination"])
	}
	for _, e := range body["data"].([]any) {
		if typ := e.(map[string]any)["event_type"].(string); typ[:10] != "bluetooth." {
			t.Fatalf("non-bluetooth event listed: %s", typ)
		}
	}
	if n := len(rows("/api/v1/bluetooth/events?type=bluetooth.system.error")); n != 1 {
		t.Fatalf("type filter: %d", n)
	}
	data("/api/v1/bluetooth/events?type=wifi.ap_observed", 400)
	data("/api/v1/bluetooth/events?bssid=64:13:AB:A5:53:E8", 400)
}

func TestBluetoothEndpointsHostGuard(t *testing.T) {
	srv, _ := newTxServer(t, false, nil)
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/bluetooth/devices", nil)
	req.Host = "evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("host guard: %d", resp.StatusCode)
	}
}

func postMode(t *testing.T, srvURL, ctype, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(srvURL+"/api/v1/bluetooth/mode", ctype, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestBluetoothModeEndpoint(t *testing.T) {
	fc := newFakeCommander()
	srv, st := newTxServer(t, false, fc)
	const js = "application/json"
	code := func(ctype, body string, want int, wantErr string) {
		t.Helper()
		c, b := postMode(t, srv.URL, ctype, body)
		if c != want {
			t.Fatalf("%s: code %d want %d: %v", body, c, want, b)
		}
		if wantErr != "" {
			if e, _ := b["error"].(map[string]any); e == nil || e["code"] != wantErr {
				t.Fatalf("%s: error %v want %s", body, b, wantErr)
			}
		}
	}
	// No sensor runs the Bluetooth build yet.
	code(js, `{"mode":"bluetooth"}`, 409, "radio_switch_unsupported")
	if fc.sentLine() != "" {
		t.Fatalf("nothing may be sent to a sensor that cannot switch: %q", fc.sentLine())
	}
	if err := st.WithTx(context.Background(), func(tx *storage.Tx) error {
		if err := tx.TouchSensor(context.Background(), "nano-70b2a8", "wraith-nano", time.Now().UnixMilli()); err != nil {
			return err
		}
		return tx.SensorStatus(context.Background(), "nano-70b2a8", "online", "0.1.0", "esp32dev",
			[]string{"serial-json", "wifi-2.4ghz", "bluetooth-le", "bluetooth-classic", "radio-switch"}, "b7b70001")
	}); err != nil {
		t.Fatal(err)
	}
	code(js, `{"mode":"bluetooth"}`, 409, "sensor_not_connected")
	fc.setRunning()

	// Input validation and the CSRF guard come before anything is sent.
	code("application/x-www-form-urlencoded", `mode=bluetooth`, 415, "unsupported_media_type")
	code("text/plain", `{"mode":"bluetooth"}`, 415, "unsupported_media_type")
	code(js, `{"mode":"BLUETOOTH"}`, 400, "invalid_body")
	code(js, `{"mode":"bluetooth","force":true}`, 400, "invalid_body")
	code(js, `not json`, 400, "invalid_body")
	if fc.sentLine() != "" {
		t.Fatalf("invalid requests sent %q", fc.sentLine())
	}

	for mode, line := range map[string]string{"bluetooth": "MODE BLUETOOTH", "wifi": "MODE WIFI", "status": "MODE STATUS"} {
		c, b := postMode(t, srv.URL, js, `{"mode":"`+mode+`"}`)
		if c != 202 || fc.sentLine() != line || b["data"].(map[string]any)["command"] != line {
			t.Fatalf("%s: %d %v sent=%q", mode, c, b, fc.sentLine())
		}
	}

	fc.mu.Lock()
	fc.err = errors.New("write: broken pipe")
	fc.mu.Unlock()
	code(js, `{"mode":"wifi"}`, 502, "command_send_failed")
}
