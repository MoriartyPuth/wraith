package bluetooth

import (
	"reflect"
	"testing"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

func TestClassFromCoD(t *testing.T) {
	for _, tc := range []struct {
		cod  int
		want string
		what string
	}{
		{0x240404, "audio", "wearable headset (audio/video major)"},
		{0x5A020C, "phone", "smartphone"},
		{0x3E010C, "computer", "laptop"},
		{0x000540, "input", "keyboard (peripheral, bit 6)"},
		{0x000580, "input", "mouse (peripheral, bit 7)"},
		{0x000508, "input", "gamepad (peripheral minor 2)"},
		{0x000518, "peripheral", "card reader (peripheral minor 6, not input)"},
		{0x000704, "wearable", "wristwatch"},
		{0x000600, "imaging", "printer/camera"},
		{0x000300, "network", "LAN access point"},
		{0x000800, "toy", "toy"},
		{0x000900, "health", "health device"},
		{0x001F00, "uncategorized", "uncategorized major"},
		{0x000000, "unknown", "miscellaneous"},
		{0x000A00, "unknown", "reserved major 10"},
	} {
		if got := ClassFromCoD(tc.cod); got != tc.want {
			t.Errorf("CoD %#06x (%s) = %q, want %q", tc.cod, tc.what, got, tc.want)
		}
	}
}

func ip(v int) *int         { return &v }
func sp(v string) *string   { return &v }
func fp(v float64) *float64 { return &v }

func TestMergeNewDevice(t *testing.T) {
	d := Merge(nil, Sighting{Transport: storage.BTBLE, Address: "11:22:33:44:55:66", AddressType: "random",
		Name: sp("Band"), RSSI: ip(-70), Services: []string{"180F", "180a", "180f"}, Count: 3,
		Seen: 1000, Sensor: "nano-1"})
	if d.FirstSeen != 1000 || d.LastSeen != 1000 || d.Observations != 3 {
		t.Fatalf("times/count: %+v", d)
	}
	if *d.Name != "Band" || *d.LastRSSI != -70 || *d.RSSIAvg != -70 || *d.LastSensor != "nano-1" {
		t.Fatalf("fields: %+v", d)
	}
	if !reflect.DeepEqual(d.Services, []string{"180a", "180f"}) {
		t.Fatalf("services normalised+deduped: %v", d.Services)
	}
	if d.DeviceClass != nil || d.Classification != nil || d.CoD != nil || d.TxPower != nil {
		t.Fatalf("unreported fields must stay nil (never invented): %+v", d)
	}
}

func TestMergeKeepsUnreportedAndAccumulates(t *testing.T) {
	prev := Merge(nil, Sighting{Transport: storage.BTBLE, Address: "A", AddressType: "public",
		Name: sp("Old"), ManufacturerID: ip(76), TxPower: ip(-4), RSSI: ip(-80), Count: 1,
		Classification: sp("beacon"), Confidence: fp(0.6), Seen: 1000})
	// A later report with fewer fields: nothing reported is cleared, empty
	// names do not overwrite, counts add, RSSI averages, first_seen stays.
	d := Merge(&prev, Sighting{Transport: storage.BTBLE, Address: "A", AddressType: "public",
		Name: sp(""), RSSI: ip(-60), Count: 2, Seen: 5000, Services: []string{"fe95"}})
	if *d.Name != "Old" || *d.ManufacturerID != 76 || *d.TxPower != -4 || *d.Classification != "beacon" {
		t.Fatalf("unreported fields lost: %+v", d)
	}
	if d.Observations != 3 || d.FirstSeen != 1000 || d.LastSeen != 5000 {
		t.Fatalf("count/times: %+v", d)
	}
	if *d.LastRSSI != -60 || *d.RSSIAvg != -75 { // -80 + 0.25*(-60 - -80)
		t.Fatalf("rssi: last=%d avg=%v", *d.LastRSSI, *d.RSSIAvg)
	}
	// Zero-count snapshot (BLE device record) adds nothing.
	d2 := Merge(&d, Sighting{Transport: storage.BTBLE, Address: "A", AddressType: "public", Seen: 6000})
	if d2.Observations != 3 || d2.LastSeen != 6000 || *d2.LastRSSI != -60 {
		t.Fatalf("zero-count snapshot: %+v", d2)
	}
}

func TestMergeClassicDerivesClassFromCoD(t *testing.T) {
	d := Merge(nil, Sighting{Transport: storage.BTClassic, Address: "B", CoD: ip(0x240404), Count: 1, Seen: 1})
	if d.DeviceClass == nil || *d.DeviceClass != "audio" {
		t.Fatalf("device_class from CoD: %+v", d.DeviceClass)
	}
	// A device_class sent by the sensor takes precedence.
	d = Merge(&d, Sighting{Transport: storage.BTClassic, Address: "B", CoD: ip(0x240404),
		DeviceClass: sp("unknown"), Count: 1, Seen: 2})
	if *d.DeviceClass != "unknown" || d.Observations != 2 {
		t.Fatalf("explicit class: %+v", d)
	}
}

func TestMergeServicesCap(t *testing.T) {
	var many []string
	for i := 0; i < MaxServices+10; i++ {
		many = append(many, string(rune('a'+i%26))+string(rune('a'+i/26))+"00")
	}
	d := Merge(nil, Sighting{Transport: storage.BTBLE, Address: "C", AddressType: "random", Services: many, Seen: 1})
	if len(d.Services) != MaxServices {
		t.Fatalf("services cap: %d", len(d.Services))
	}
}
