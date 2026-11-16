package btident

import "testing"

func ip(v int) *int       { return &v }
func sp(v string) *string { return &v }
func pct(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestDecodeApple(t *testing.T) {
	apple := ip(0x004C)
	cases := []struct {
		name, data, kind, label, tracker string
		rank                             int
		separated                        bool
	}{
		// Real payloads captured by nano-70b2a8 (2026-11).
		{"find my near owner", "12020003", KindFindMy, "Find My accessory near its owner (AirTag, AirPods or similar)", TrackerApple, 40, false},
		{"nearby info", "1007351f1e64b3b818", KindPhone, "iPhone, iPad, Mac or Apple Watch", "", 45, false},
		{"handoff", "0c0e00a7df5b3a2d0c4a1bb2c5e1f4d2", KindPhone, "iPhone, iPad or Mac", "", 50, false},
		{"airplay target + extra TLV", "09081302c0a801061b581608000ac697873ca752", KindTV, "AirPlay receiver (Apple TV, HomePod or Mac)", "", 60, false},
		{"nearby action", "0f09a033f458100495cb0f1003480d18", KindPhone, "Apple device (setup or sharing prompt)", "", 45, false},
		{"airplay source", "0a0100", KindPhone, "iPhone, iPad or Mac (AirPlay source)", "", 45, false},
		// Constructed.
		{"find my separated", "1219" + "10" + "00112233445566778899aabbccddeeff00112233" + "0100", KindTracker,
			"Find My accessory away from its owner (AirTag, AirPods or similar)", TrackerApple, 80, true},
		{"magic switch", "0b0500112233", KindWatch, "Apple Watch", "", 65, false},
		{"unknown type only", "16080000", KindOther, "Apple device", "", 10, false},
		{"empty", "", KindOther, "Apple device", "", 10, false},
		{"bad hex", "zz", KindOther, "Apple device", "", 10, false},
		{"truncated TLV", "0c", KindOther, "Apple device", "", 10, false},
	}
	for _, c := range cases {
		id := Decode(apple, sp(c.data), nil)
		if id == nil || id.Kind != c.kind || id.Label != c.label || id.Tracker != c.tracker || id.Rank != c.rank || id.Separated != c.separated {
			t.Errorf("%s: got %+v", c.name, id)
		}
	}
}

func TestProximityPairing(t *testing.T) {
	// AirPods Pro 2: status 0x2b (bit 0x20 set → not flipped: low nibble = left),
	// battery 0x97 → right 90 %, left 70 %; charging nibble 0x5 → left (bit 0) and
	// case (bit 2) charging; case 0x8 → 80 %.
	id := Decode(ip(0x004C), sp("0719"+"01"+"1420"+"2b"+"97"+"58"+"03"+"00"+"00"+"00112233445566778899aabbccddeeff"), nil)
	if id == nil || id.Kind != KindHeadphones || id.Label != "AirPods Pro (2nd gen)" || id.Rank != 90 || id.Battery == nil {
		t.Fatalf("got %+v", id)
	}
	b := id.Battery
	if pct(b.Left) != 70 || pct(b.Right) != 90 || pct(b.Case) != 80 || !b.LeftCharging || b.RightCharging || !b.CaseCharging {
		t.Fatalf("battery %+v left=%v right=%v case=%v", b, pct(b.Left), pct(b.Right), pct(b.Case))
	}
	// Flipped (status bit 0x20 clear): high nibble = left. 0xF = not reported.
	id = Decode(ip(0x004C), sp("0719"+"01"+"0e20"+"0b"+"a3"+"1f"), nil)
	if id.Label != "AirPods Pro" || pct(id.Battery.Left) != 100 || pct(id.Battery.Right) != 30 ||
		id.Battery.Case != nil || id.Battery.LeftCharging || !id.Battery.RightCharging {
		t.Fatalf("flipped: %+v %+v", id, id.Battery)
	}
	// Unknown model keeps the id visible; nothing reported → no battery.
	id = Decode(ip(0x004C), sp("0719"+"01"+"9920"+"2b"+"ff"+"0f"), nil)
	if id.Label != "AirPods or Beats (model 0x9920)" || id.Rank != 70 || id.Battery != nil {
		t.Fatalf("unknown model: %+v", id)
	}
	// Too short for a model.
	if id = Decode(ip(0x004C), sp("071901"), nil); id.Label != "AirPods or Beats" {
		t.Fatalf("short: %+v", id)
	}
}

func TestIBeacon(t *testing.T) {
	id := Decode(ip(0x004C), sp("0215"+"f7826da64fa24e988024bc5b71e0893e"+"0001"+"00ff"+"c5"), nil)
	if id == nil || id.Kind != KindBeacon || id.Label != "iBeacon f7826da6-4fa2-4e98-8024-bc5b71e0893e major 1 minor 255" {
		t.Fatalf("got %+v", id)
	}
}

func TestServicesAndOthers(t *testing.T) {
	cases := []struct {
		services            []string
		mfr                 *int
		data, kind, tracker string
		separated           bool
	}{
		{[]string{"feed"}, nil, "", KindTracker, TrackerTile, false},
		{[]string{"FD84"}, nil, "", KindTracker, TrackerTile, false}, // case-insensitive
		{[]string{"fe33"}, nil, "", KindTracker, TrackerChipolo, false},
		{[]string{"fd5a"}, nil, "", KindTracker, TrackerSamsung, false},
		{[]string{"fcb2"}, nil, "", KindTracker, TrackerDULT, true},
		{[]string{"fe2c"}, nil, "", KindHeadphones, "", false},
		{[]string{"feaa"}, nil, "", KindBeacon, "", false},
		{nil, ip(0x0006), "030080aa", KindOther, "", false},
		// The more specific signal wins: separated DULT tracker over Fast Pair.
		{[]string{"fe2c", "fcb2"}, nil, "", KindTracker, TrackerDULT, true},
		// Apple separated Find My (80) loses to the DULT advert (88) in the same packet.
		{[]string{"fcb2"}, ip(0x004C), "1219" + "00112233445566778899aabbccddeeff00112233445566", KindTracker, TrackerDULT, true},
	}
	for i, c := range cases {
		var data *string
		if c.data != "" {
			data = sp(c.data)
		}
		id := Decode(c.mfr, data, c.services)
		if id == nil || id.Kind != c.kind || id.Tracker != c.tracker || id.Separated != c.separated {
			t.Errorf("case %d %v: got %+v", i, c.services, id)
		}
	}
	// Nothing recognisable.
	for _, s := range [][]string{nil, {"feb9"}, {"5555"}, {"fdee"}} {
		if id := Decode(nil, nil, s); id != nil {
			t.Errorf("%v: want nil, got %+v", s, id)
		}
	}
	if id := Decode(ip(0x0075), sp("0102"), nil); id != nil { // Samsung manufacturer data alone says nothing
		t.Errorf("samsung mfr: %+v", id)
	}
}

func TestBetter(t *testing.T) {
	hi, lo := &Identity{Rank: 90}, &Identity{Rank: 40}
	if !Better(lo, nil) || !Better(hi, lo) || Better(lo, hi) || !Better(&Identity{Rank: 90}, hi) || Better(nil, lo) {
		t.Fatal("ordering")
	}
	if (&Identity{Kind: KindFindMy}).IsTracker() || !(&Identity{Kind: KindTracker}).IsTracker() || (*Identity)(nil).IsTracker() {
		t.Fatal("IsTracker")
	}
}
