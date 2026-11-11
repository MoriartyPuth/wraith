package system

import (
	"reflect"
	"testing"
)

// Trimmed real output of `iw phy phy0 info` from an MT7922 (mt7921e).
const mt7922Phy = `Wiphy phy0
	wiphy index: 0
	max # scan SSIDs: 4
	Supported interface modes:
		 * managed
		 * AP
		 * AP/VLAN
		 * monitor
		 * P2P-client
		 * P2P-GO
		 * P2P-device
	Band 1:
		Capabilities: 0x1ff
	valid interface combinations:
		 * #{ managed, P2P-client } <= 2, #{ P2P-GO } <= 1, #{ P2P-device } <= 1,
		   total <= 3, #channels <= 2
		 * #{ managed, P2P-client } <= 2, #{ AP } <= 1, #{ P2P-device } <= 1,
		   total <= 3, #channels <= 1
	HT Capability overrides:
		 * MCS: ff ff ff ff ff ff ff ff ff ff
`

func TestParseIWPhyMT7922(t *testing.T) {
	modes, combo := parseIWPhy(mt7922Phy)
	want := []string{"managed", "AP", "AP/VLAN", "monitor", "P2P-client", "P2P-GO", "P2P-device"}
	if !reflect.DeepEqual(modes, want) {
		t.Fatalf("modes = %v", modes)
	}
	if combo {
		t.Fatal("MT7922 does not advertise a monitor interface combination")
	}
}

func TestParseIWPhyMonitorCombo(t *testing.T) {
	out := "Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * monitor\n\tvalid interface combinations:\n\t\t * #{ managed } <= 1, #{ monitor } <= 1,\n\t\t   total <= 2, #channels <= 1\n"
	_, combo := parseIWPhy(out)
	if !combo {
		t.Fatal("monitor combination not detected")
	}
}

func TestParseIWDevMode(t *testing.T) {
	out := "Interface wlo1\n\tifindex 3\n\ttype managed\n\tchannel 36 (5180 MHz)\n"
	if m := parseIWDevMode(out); m != "managed" {
		t.Fatalf("mode = %q", m)
	}
}

func TestInterfaceNameValidation(t *testing.T) {
	for _, bad := range []string{"", "../etc", "a/b", "."} {
		if _, err := WirelessInterfaceInfo(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
