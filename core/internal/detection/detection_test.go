package detection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

type fakeLookup struct{ aps []storage.AccessPoint }

func (f *fakeLookup) APsBySSIDHex(hex string) ([]storage.AccessPoint, error) {
	var out []storage.AccessPoint
	for _, a := range f.aps {
		if a.SSIDHex == hex {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeLookup) GetAP(b string) (*storage.AccessPoint, error) {
	for i := range f.aps {
		if f.aps[i].BSSID == b {
			return &f.aps[i], nil
		}
	}
	return nil, nil
}

var seq int

func ev(typ, bssid string, meta any) *events.Event {
	seq++
	m, _ := json.Marshal(meta)
	return &events.Event{Version: 1, EventID: fmt.Sprintf("b-%d", seq), Source: events.SourceNano,
		SensorID: "nano-1", Type: typ, BSSID: bssid, Metadata: m}
}

func ap(bssid, ssid, auth, pmf string) *storage.AccessPoint {
	a := auth
	return &storage.AccessPoint{BSSID: bssid, SSID: ssid, SSIDHex: fmt.Sprintf("%x", ssid), AuthMode: &a, PMF: pmf}
}

func mgmt(count int, dst string, protected bool, reasons ...int) map[string]any {
	return map[string]any{"count": count, "window_ms": 1000, "dst": dst, "protected": protected, "reason_codes": reasons}
}

const B = "AA:BB:CC:DD:EE:01"

func TestDeauthBelowThresholdIsSilent(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	for i := 0; i < 29; i++ { // 29 frames in 29 s: under the 30/60 s threshold
		f := e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(1, "11:22:33:44:55:66", false, 7)), Now: now.Add(time.Duration(i) * time.Second)})
		if len(f) != 0 {
			t.Fatalf("finding for ordinary deauth traffic: %+v", f)
		}
	}
}

func TestDeauthWindowExpires(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(20, "FF:FF:FF:FF:FF:FF", false, 7)), Now: now})
	// 20 more, but 61 s later: the first 20 have left the window.
	f := e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(20, "FF:FF:FF:FF:FF:FF", false, 7)), Now: now.Add(61 * time.Second)})
	if len(f) != 0 {
		t.Fatalf("stale frames counted: %+v", f)
	}
}

func TestDeauthBurstSuspicious(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	var got []storage.Finding
	for i := 0; i < 4; i++ {
		got = append(got, e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(10, "FF:FF:FF:FF:FF:FF", false, 7, 1)),
			CurAP: ap(B, "Lab", "wpa2-psk", "capable"), Now: now.Add(time.Duration(i) * time.Second)})...)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 finding (cooldown), got %d", len(got))
	}
	f := got[0]
	if f.Classification != ClassSuspicious || f.Severity != "medium" || f.Category != "deauth_activity" {
		t.Fatalf("unexpected classification: %+v", f)
	}
	for _, want := range []string{"30 deauthentication", "reason codes [1 7]", "broadcast", B} {
		if !strings.Contains(f.Explanation, want) {
			t.Errorf("explanation missing %q: %s", want, f.Explanation)
		}
	}
	if f.Limitations == "" || len(f.Evidence) != 3 || *f.BSSID != B || *f.SensorID != "nano-1" {
		t.Fatalf("finding incomplete: %+v", f)
	}
}

func TestDeauthUnprotectedOnPMFRequiredIsPotentialAttack(t *testing.T) {
	e := NewEngine()
	f := e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(120, "FF:FF:FF:FF:FF:FF", false, 7)),
		CurAP: ap(B, "Lab", "wpa3-sae", "required"), Now: time.Now()})
	if len(f) != 1 || f[0].Classification != ClassPotential || f[0].Severity != "high" {
		t.Fatalf("want potential_attack/high, got %+v", f)
	}
	// Same volume but the AP only advertises PMF capable: not escalated to potential_attack.
	e = NewEngine()
	f = e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(120, "FF:FF:FF:FF:FF:FF", false, 7)),
		CurAP: ap(B, "Lab", "wpa2-psk", "capable"), Now: time.Now()})
	if len(f) != 1 || f[0].Classification != ClassSuspicious || f[0].Severity != "high" {
		t.Fatalf("want suspicious/high, got %+v", f)
	}
}

func TestNeverConfirmed(t *testing.T) {
	e := NewEngine()
	f := e.Observe(&Context{Event: ev(events.TypeDeauthObserved, B, mgmt(5000, "FF:FF:FF:FF:FF:FF", false, 7)),
		CurAP: ap(B, "Lab", "wpa3-sae", "required"), Now: time.Now()})
	for _, x := range f {
		if x.Classification == "confirmed" {
			t.Fatal("detectors must never confirm")
		}
	}
}

func TestSecurityDowngradeAndUpgrade(t *testing.T) {
	e := NewEngine()
	now := time.Now()
	f := e.Observe(&Context{Event: ev(events.TypeAPObserved, B, nil), PrevAP: ap(B, "Lab", "wpa2-psk", "capable"),
		CurAP: ap(B, "Lab", "open", "not_advertised"), Now: now})
	if len(f) != 1 || f[0].Category != "security_downgrade" || f[0].Severity != "high" {
		t.Fatalf("downgrade to open: %+v", f)
	}
	if !strings.Contains(f[0].Explanation, "from wpa2-psk to open") {
		t.Errorf("explanation: %s", f[0].Explanation)
	}
	f = e.Observe(&Context{Event: ev(events.TypeAPObserved, "AA:00:00:00:00:02", nil), PrevAP: ap("AA:00:00:00:00:02", "X", "wpa2-psk", "capable"),
		CurAP: ap("AA:00:00:00:00:02", "X", "wpa3-sae", "required"), Now: now})
	if len(f) != 0 {
		t.Fatalf("upgrade produced a finding: %+v", f)
	}
}

func TestPMFDowngrade(t *testing.T) {
	e := NewEngine()
	f := e.Observe(&Context{Event: ev(events.TypePMFObserved, B, nil), PrevAP: ap(B, "Lab", "wpa3-sae", "required"),
		CurAP: ap(B, "Lab", "wpa3-sae", "capable"), Now: time.Now()})
	if len(f) != 1 || f[0].Category != "pmf_downgrade" {
		t.Fatalf("pmf downgrade: %+v", f)
	}
	// "unavailable" is not ranked: no finding.
	f = e.Observe(&Context{Event: ev(events.TypePMFObserved, "AA:00:00:00:00:09", nil), PrevAP: ap("AA:00:00:00:00:09", "Lab", "wpa2-psk", "capable"),
		CurAP: ap("AA:00:00:00:00:09", "Lab", "wpa2-psk", "unavailable"), Now: time.Now()})
	if len(f) != 0 {
		t.Fatalf("unavailable treated as downgrade: %+v", f)
	}
}

func TestSSIDChangeObserved(t *testing.T) {
	e := NewEngine()
	f := e.Observe(&Context{Event: ev(events.TypeAPObserved, B, nil), PrevAP: ap(B, "Lab", "wpa2-psk", "capable"),
		CurAP: ap(B, "Lab2", "wpa2-psk", "capable"), Now: time.Now()})
	if len(f) != 1 || f[0].Category != "ap_identity_change" || f[0].Classification != ClassObserved {
		t.Fatalf("ssid change: %+v", f)
	}
}

func TestRogueAPIndicator(t *testing.T) {
	lk := &fakeLookup{aps: []storage.AccessPoint{*ap("AA:00:00:00:00:01", "Home", "wpa2-psk", "capable"),
		*ap("AA:00:00:00:00:02", "Home", "wpa2-psk", "capable")}}
	e := NewEngine()

	// Mesh node: same SSID, same security → nothing.
	mesh := ap("AA:00:00:00:00:03", "Home", "wpa2-psk", "capable")
	lk.aps = append(lk.aps, *mesh)
	if f := e.Observe(&Context{Event: ev(events.TypeAPObserved, mesh.BSSID, nil), CurAP: mesh, Lookup: lk, Now: time.Now()}); len(f) != 0 {
		t.Fatalf("mesh node flagged: %+v", f)
	}

	// Open twin of a WPA2 network → suspicious/high.
	twin := ap("02:11:22:33:44:55", "Home", "open", "not_advertised")
	lk.aps = append(lk.aps, *twin)
	f := e.Observe(&Context{Event: ev(events.TypeAPObserved, twin.BSSID, nil), CurAP: twin, Lookup: lk, Now: time.Now()})
	if len(f) != 1 || f[0].Category != "rogue_ap_indicator" || f[0].Severity != "high" || f[0].Classification != ClassSuspicious {
		t.Fatalf("open twin: %+v", f)
	}
	if !strings.Contains(f[0].Explanation, "AA:00:00:00:00:01 (wpa2-psk)") || !strings.Contains(f[0].Limitations, "mesh") {
		t.Errorf("explanation/limitations: %s | %s", f[0].Explanation, f[0].Limitations)
	}

	// Known BSSID with known auth mode is not re-evaluated.
	if f := e.Observe(&Context{Event: ev(events.TypeAPObserved, twin.BSSID, nil), PrevAP: twin, CurAP: twin, Lookup: lk, Now: time.Now()}); len(f) != 0 {
		t.Fatalf("known BSSID re-flagged: %+v", f)
	}

	// Hidden SSIDs are never compared.
	hidden := &storage.AccessPoint{BSSID: "02:00:00:00:00:09", SSIDHex: "000000", AuthMode: twin.AuthMode}
	if f := e.Observe(&Context{Event: ev(events.TypeAPObserved, hidden.BSSID, nil), CurAP: hidden, Lookup: lk, Now: time.Now()}); len(f) != 0 {
		t.Fatalf("hidden flagged: %+v", f)
	}
}

// Real-hardware ordering: a beacon report creates the row (auth unknown)
// before ap_observed supplies the auth mode. The AP must still be evaluated.
func TestRogueEvaluatedWhenAuthBecomesKnown(t *testing.T) {
	strong := ap("FC:00:00:00:00:01", "Rozas", "wpa2-psk", "not_advertised")
	weak := ap("F8:00:00:00:00:02", "Rozas", "wpa-wpa2-psk", "not_advertised")
	lk := &fakeLookup{aps: []storage.AccessPoint{*strong, *weak}}
	rowFromBeacon := &storage.AccessPoint{BSSID: weak.BSSID, SSID: "Rozas", SSIDHex: weak.SSIDHex, PMF: "unavailable"}
	f := NewEngine().Observe(&Context{Event: ev(events.TypeAPObserved, weak.BSSID, nil), PrevAP: rowFromBeacon, CurAP: weak, Lookup: lk, Now: time.Now()})
	if len(f) != 1 || *f[0].BSSID != weak.BSSID || f[0].Severity != "low" {
		t.Fatalf("one-step-weaker sibling: %+v", f)
	}
}

// Weaker AP identified first, stronger sibling later: the weaker one is
// flagged when the stronger one becomes known.
func TestRogueFlagsEarlierWeakerSibling(t *testing.T) {
	weak := ap("02:00:00:00:00:0A", "Cafe", "open", "not_advertised")
	strong := ap("AA:00:00:00:00:0B", "Cafe", "wpa2-psk", "capable")
	lk := &fakeLookup{aps: []storage.AccessPoint{*weak}}
	e := NewEngine()
	if f := e.Observe(&Context{Event: ev(events.TypeAPObserved, weak.BSSID, nil), CurAP: weak, Lookup: lk, Now: time.Now()}); len(f) != 0 {
		t.Fatalf("lone open AP flagged: %+v", f)
	}
	lk.aps = append(lk.aps, *strong)
	f := e.Observe(&Context{Event: ev(events.TypeAPObserved, strong.BSSID, nil), CurAP: strong, Lookup: lk, Now: time.Now()})
	if len(f) != 1 || *f[0].BSSID != weak.BSSID || f[0].Severity != "high" {
		t.Fatalf("earlier weaker sibling not flagged: %+v", f)
	}
}

func TestPMFInvalidOncePerBSSID(t *testing.T) {
	e := NewEngine()
	meta := map[string]any{"ssid": "x", "ssid_hex": "78", "rssi": -40,
		"rsn": map[string]any{"version": 1, "group_cipher": "ccmp-128", "pairwise": []string{}, "akm": []string{}, "mfpc": false, "mfpr": true}}
	f := e.Observe(&Context{Event: ev(events.TypeBeaconObserved, B, meta), Now: time.Now()})
	if len(f) != 1 || f[0].Category != "pmf_invalid_advertisement" || f[0].Severity != "info" {
		t.Fatalf("invalid pmf: %+v", f)
	}
	if f := e.Observe(&Context{Event: ev(events.TypeBeaconObserved, B, meta), Now: time.Now()}); len(f) != 0 {
		t.Fatal("reported twice")
	}
}
