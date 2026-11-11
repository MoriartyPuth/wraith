package events

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureDir = "../../../protocol/fixtures"

func TestValidFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.json"))
	if len(files) == 0 {
		t.Fatal("no valid fixtures found")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := Parse([]byte(strings.TrimSpace(string(raw))))
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !SupportedVersions[int64(ev.Version)] || ev.EventID == "" || ev.SensorID == "" {
				t.Fatalf("decoded envelope incomplete: %+v", ev)
			}
		})
	}
}

func TestInvalidFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*.json"))
	if len(files) == 0 {
		t.Fatal("no invalid fixtures found")
	}
	want := map[string]string{
		"version.json":                 ReasonUnsupportedVer,
		"unknown_field.json":           ReasonUnknownField,
		"unknown_meta.json":            ReasonUnknownField,
		"missing_bssid.json":           ReasonMissingField,
		"missing_meta.json":            ReasonMissingField,
		"bad_event_type.json":          ReasonUnknownEventType,
		"not_object.json":              ReasonMalformedJSON,
		"client_bad_frames.json":       ReasonInvalidField,
		"client_bad_mac.json":          ReasonInvalidField,
		"client_missing_bssid.json":    ReasonMissingField,
		"tx_ack_bad_status.json":       ReasonInvalidField,
		"tx_report_unknown_field.json": ReasonUnknownField,
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			raw, _ := os.ReadFile(f)
			_, err := Parse([]byte(strings.TrimSpace(string(raw))))
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if r, ok := want[name]; ok && ve.Reason != r {
				t.Fatalf("reason = %s (%v), want %s", ve.Reason, ve, r)
			}
		})
	}
}

func TestOversize(t *testing.T) {
	big := `{"x":"` + strings.Repeat("a", MaxMessageSize) + `"}`
	_, err := Parse([]byte(big))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Reason != ReasonOversize {
		t.Fatalf("want oversize, got %v", err)
	}
}

func TestRejectsTrailingDataAndBadUTF8(t *testing.T) {
	ok := `{"version":1,"event_id":"a-1","source":"wraith-nano","sensor_id":"n","event_type":"sensor.heartbeat","timestamp":0,"seq":0,"metadata":{"uptime_ms":1,"free_heap":1,"queue_drops":0}}`
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("baseline should pass: %v", err)
	}
	if _, err := Parse([]byte(ok + ` {}`)); err == nil {
		t.Fatal("trailing object accepted")
	}
	if _, err := Parse([]byte(strings.Replace(ok, `"n"`, "\"\xff\"", 1))); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := Parse([]byte(strings.Replace(ok, `"seq":0`, `"seq":1.5`, 1))); err == nil {
		t.Fatal("non-integer seq accepted")
	}
}

func TestSSIDByteLimit(t *testing.T) {
	// 11 Khmer characters = 33 bytes UTF-8: over the 32-byte SSID limit.
	ssid := strings.Repeat("ក", 11)
	raw := `{"version":1,"event_id":"a-1","source":"wraith-nano","sensor_id":"n","event_type":"wifi.ap_observed","timestamp":0,"seq":0,"channel":1,"bssid":"AA:BB:CC:DD:EE:FF","metadata":{"ssid":"` + ssid + `","ssid_hex":"","rssi":-40}}`
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("33-byte SSID accepted")
	}
}

func TestVersionedEventTypes(t *testing.T) {
	const ack = `{"version":%d,"event_id":"a-1","source":"wraith-nano","sensor_id":"n","event_type":"tx.ack","timestamp":0,"seq":0,"metadata":{"command":"deauth","status":"accepted"}}`
	if _, err := Parse([]byte(fmt.Sprintf(ack, 2))); err != nil {
		t.Fatalf("v2 tx.ack: %v", err)
	}
	_, err := Parse([]byte(fmt.Sprintf(ack, 1)))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Reason != ReasonUnknownEventType {
		t.Fatalf("v1 tx.ack: %v", err)
	}
	const ap = `{"version":%d,"event_id":"a-1","source":"wraith-nano","sensor_id":"n","event_type":"wifi.ap_observed","timestamp":0,"seq":0,"channel":1,"bssid":"AA:BB:CC:DD:EE:FF","metadata":{"ssid":"ssid","ssid_hex":"","rssi":-40}}`
	if _, err := Parse([]byte(fmt.Sprintf(ap, 1))); err != nil {
		t.Fatalf("v1 ap_observed: %v", err)
	}
	if _, err := Parse([]byte(fmt.Sprintf(ap, 2))); err != nil {
		t.Fatalf("v2 still accepts v1 types: %v", err)
	}
}

func TestClientEventVersionGate(t *testing.T) {
	base := `{"version":%d,"event_id":"a-1","source":"wraith-nano","sensor_id":"n","event_type":"wifi.client_observed","timestamp":0,"seq":0,"channel":6,"bssid":"AA:BB:CC:DD:EE:FF","metadata":{"client":"02:AA:BB:CC:DD:EE","frames":3,"window_ms":100}}`
	if _, err := Parse([]byte(fmt.Sprintf(base, 3))); err != nil {
		t.Fatalf("v3 client: %v", err)
	}
	_, err := Parse([]byte(fmt.Sprintf(base, 2)))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Reason != ReasonUnknownEventType {
		t.Fatalf("v2 client: %v", err)
	}
}
