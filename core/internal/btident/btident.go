// Package btident works out what a BLE advertiser is from what it
// broadcasts: Apple Continuity messages inside Apple manufacturer data, and
// 16-bit service UUIDs registered to tracker makers (Bluetooth SIG member
// UUIDs). Everything is decoded from the advertisement itself; nothing is
// guessed from the address, and nothing links one address to another.
//
// Apple Continuity layout (public reverse-engineering: furiousMAC
// "continuity", OpenPods for AirPods battery): the manufacturer data after
// the company id 0x004C is a sequence of TLVs, [type][length][value…].
//
// Deliberately not decoded: Nearby Info activity codes (screen on, driving,
// on a call…) and the AirPlay target's IP address. They describe what nearby
// people are doing or where their devices sit on their network, which is not
// needed to name a device class.
package btident

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// Kinds (Identity.Kind).
const (
	KindHeadphones = "headphones"
	KindPhone      = "phone_tablet_computer"
	KindWatch      = "watch"
	KindTV         = "tv_speaker"
	KindTracker    = "tracker"
	KindFindMy     = "find_my"
	KindBeacon     = "beacon"
	KindHome       = "smart_home"
	KindOther      = "other"
)

// Tracker brands (Identity.Tracker).
const (
	TrackerApple   = "apple_find_my"
	TrackerTile    = "tile"
	TrackerSamsung = "samsung"
	TrackerChipolo = "chipolo"
	TrackerDULT    = "dult" // any maker's tracker in the cross-platform unwanted-tracking (separated) mode
)

// Battery is AirPods/Beats battery state from Proximity Pairing. Levels are
// percentages in 10 % steps; nil means not reported (e.g. bud in the case,
// case closed).
type Battery struct {
	Left          *int `json:"left,omitempty"`
	Right         *int `json:"right,omitempty"`
	Case          *int `json:"case,omitempty"`
	LeftCharging  bool `json:"left_charging,omitempty"`
	RightCharging bool `json:"right_charging,omitempty"`
	CaseCharging  bool `json:"case_charging,omitempty"`
}

// Identity is what one advertisement says about its sender.
type Identity struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`  // human description, e.g. "AirPods Pro (2nd gen)"
	Source string `json:"source"` // what it was decoded from, e.g. "Apple Continuity: Proximity Pairing"
	// Rank orders identities by specificity; a device keeps its most specific
	// identity (see Better).
	Rank int `json:"rank"`
	// Tracker is the tracker brand for item trackers ("" otherwise).
	Tracker string `json:"tracker,omitempty"`
	// Separated: the tracker is broadcasting its away-from-owner advert.
	Separated bool     `json:"separated,omitempty"`
	Battery   *Battery `json:"battery,omitempty"`
	// At is when the identity was last confirmed (Unix ms), set by the caller.
	At int64 `json:"at,omitempty"`
}

// Better reports whether n should replace the stored identity cur: a more
// specific identity always wins; an equally specific one refreshes it (newer
// battery levels, tracker state). A less specific message (an iPhone's Nearby
// Info after its Handoff, AirPods' Find My advert after Proximity Pairing)
// never downgrades what is known.
func Better(n, cur *Identity) bool {
	return n != nil && (cur == nil || n.Rank >= cur.Rank)
}

// IsTracker reports an item-tracker identity (any brand). Apple's near-owner
// Find My advert is not one: its owner is nearby by definition.
func (i *Identity) IsTracker() bool { return i != nil && i.Kind == KindTracker }

const (
	appleCompany     = 0x004C
	microsoftCompany = 0x0006
)

// Decode returns the most specific identity found in one advertisement, or
// nil. mfrData is hex (as in the event metadata), services lowercase hex
// UUIDs.
func Decode(mfrID *int, mfrData *string, services []string) *Identity {
	var best *Identity
	consider := func(i *Identity) {
		if i != nil && (best == nil || i.Rank > best.Rank) {
			best = i
		}
	}
	for _, u := range services {
		consider(fromService(strings.ToLower(u)))
	}
	if mfrID != nil {
		var data []byte
		if mfrData != nil {
			data, _ = hex.DecodeString(*mfrData) // invalid hex: treat as absent
		}
		switch *mfrID {
		case appleCompany:
			consider(apple(data))
		case microsoftCompany:
			if len(data) >= 3 && data[0] == 0x03 && data[1] == 0x00 && data[2] == 0x80 {
				consider(&Identity{Kind: KindOther, Label: "Windows Swift Pair device (mouse, keyboard or headset)",
					Source: "Microsoft Swift Pair", Rank: 50})
			}
		}
	}
	return best
}

// fromService maps registered 16-bit service UUIDs (Bluetooth SIG member
// UUIDs, verified against the assigned-numbers registry).
func fromService(u string) *Identity {
	switch u {
	case "feed", "feec", "fd84": // Tile, Inc.
		return &Identity{Kind: KindTracker, Label: "Tile tracker", Source: "service UUID 0x" + u + " (Tile)",
			Rank: 85, Tracker: TrackerTile}
	case "fe33": // CHIPOLO d.o.o.
		return &Identity{Kind: KindTracker, Label: "Chipolo tracker", Source: "service UUID 0xfe33 (Chipolo)",
			Rank: 85, Tracker: TrackerChipolo}
	case "fcb2": // Apple Inc., used by the cross-platform unwanted-tracking (DULT) separated advert
		return &Identity{Kind: KindTracker, Label: "Item tracker away from its owner (any brand)",
			Source: "service data 0xfcb2 (unwanted-tracking alert)", Rank: 88, Tracker: TrackerDULT, Separated: true}
	case "fd5a": // Samsung: SmartThings Find
		return &Identity{Kind: KindTracker, Label: "Samsung SmartThings Find (SmartTag or other Galaxy device)",
			Source: "service UUID 0xfd5a (Samsung)", Rank: 55, Tracker: TrackerSamsung}
	case "fe2c": // Google: Fast Pair
		return &Identity{Kind: KindHeadphones, Label: "Google Fast Pair device (earbuds, headphones or speaker)",
			Source: "service UUID 0xfe2c (Google Fast Pair)", Rank: 50}
	case "feaa": // Google: Eddystone (also used by the Find My Device network)
		return &Identity{Kind: KindBeacon, Label: "Eddystone beacon or Google Find My Device accessory",
			Source: "service UUID 0xfeaa (Google)", Rank: 40}
	}
	return nil
}

// apple decodes Continuity TLVs and returns the most specific message.
func apple(data []byte) *Identity {
	var best *Identity
	for i := 0; i+2 <= len(data); {
		typ, n := data[i], int(data[i+1])
		v := data[i+2 : min(i+2+n, len(data))] // a capped payload (≤ 32 bytes) may truncate the last TLV
		if id := appleMessage(typ, v, n); id != nil && (best == nil || id.Rank > best.Rank) {
			best = id
		}
		i += 2 + n
	}
	if best == nil {
		best = &Identity{Kind: KindOther, Label: "Apple device", Source: "Apple manufacturer data", Rank: 10}
	}
	return best
}

func appleMessage(typ byte, v []byte, declared int) *Identity {
	src := func(name string) string { return "Apple Continuity: " + name }
	switch typ {
	case 0x02:
		if declared == 0x15 && len(v) >= 20 {
			return &Identity{Kind: KindBeacon, Rank: 70, Source: src("iBeacon"),
				Label: fmt.Sprintf("iBeacon %s major %d minor %d", uuidText(v[0:16]),
					int(v[16])<<8|int(v[17]), int(v[18])<<8|int(v[19]))}
		}
	case 0x05:
		return &Identity{Kind: KindPhone, Label: "iPhone, iPad or Mac (AirDrop)", Source: src("AirDrop"), Rank: 50}
	case 0x06:
		return &Identity{Kind: KindHome, Label: "HomeKit accessory", Source: src("HomeKit"), Rank: 60}
	case 0x07:
		return proximityPairing(v)
	case 0x09:
		return &Identity{Kind: KindTV, Label: "AirPlay receiver (Apple TV, HomePod or Mac)", Source: src("AirPlay Target"), Rank: 60}
	case 0x0A:
		return &Identity{Kind: KindPhone, Label: "iPhone, iPad or Mac (AirPlay source)", Source: src("AirPlay Source"), Rank: 45}
	case 0x0B:
		return &Identity{Kind: KindWatch, Label: "Apple Watch", Source: src("Magic Switch"), Rank: 65}
	case 0x0C:
		return &Identity{Kind: KindPhone, Label: "iPhone, iPad or Mac", Source: src("Handoff"), Rank: 50}
	case 0x0D:
		return &Identity{Kind: KindPhone, Label: "iPhone, iPad or Mac (looking for a hotspot)", Source: src("Instant Hotspot"), Rank: 45}
	case 0x0E:
		return &Identity{Kind: KindPhone, Label: "iPhone or cellular iPad (offering Instant Hotspot)", Source: src("Instant Hotspot"), Rank: 60}
	case 0x0F:
		return &Identity{Kind: KindPhone, Label: "Apple device (setup or sharing prompt)", Source: src("Nearby Action"), Rank: 45}
	case 0x10:
		return &Identity{Kind: KindPhone, Label: "iPhone, iPad, Mac or Apple Watch", Source: src("Nearby Info"), Rank: 45}
	case 0x12:
		if declared == 0x19 { // 25-byte payload: separated from its owner
			return &Identity{Kind: KindTracker, Label: "Find My accessory away from its owner (AirTag, AirPods or similar)",
				Source: src("Find My (separated)"), Rank: 80, Tracker: TrackerApple, Separated: true}
		}
		return &Identity{Kind: KindFindMy, Label: "Find My accessory near its owner (AirTag, AirPods or similar)",
			Source: src("Find My (near owner)"), Rank: 40, Tracker: TrackerApple}
	}
	return nil
}

// AirPods/Beats model ids (Proximity Pairing bytes 1-2, as transmitted).
var models = map[string]string{
	"0220": "AirPods (1st gen)", "0f20": "AirPods (2nd gen)", "1320": "AirPods (3rd gen)",
	"0e20": "AirPods Pro", "1420": "AirPods Pro (2nd gen)", "2420": "AirPods Pro (2nd gen, USB-C)",
	"0a20": "AirPods Max",
	"0320": "Powerbeats3", "0b20": "Powerbeats Pro", "0520": "BeatsX", "0620": "Beats Solo3",
	"0920": "Beats Studio3", "0c20": "Beats Solo Pro", "1020": "Beats Flex", "1120": "Beats Studio Buds",
	"1220": "Beats Fit Pro", "1620": "Beats Studio Buds+", "1720": "Beats Studio Pro",
}

// proximityPairing decodes the AirPods/Beats status advert (sent while the
// case is open or the buds are in use): [prefix][model 2][status]
// [battery: two nibbles][charging nibble | case nibble][lid][color]…
func proximityPairing(v []byte) *Identity {
	id := &Identity{Kind: KindHeadphones, Source: "Apple Continuity: Proximity Pairing", Rank: 70,
		Label: "AirPods or Beats"}
	if len(v) < 3 {
		return id
	}
	model := hex.EncodeToString(v[1:3])
	if name, ok := models[model]; ok {
		id.Label, id.Rank = name, 90
	} else {
		id.Label = "AirPods or Beats (model 0x" + model + ")"
	}
	if len(v) < 6 {
		return id
	}
	status, batt, chg := v[3], v[4], v[5]
	// "Flipped" (status bit 0x20 clear): the high battery nibble is the left
	// bud, otherwise the right one (OpenPods).
	flip := status&0x20 == 0
	hi, lo := level(batt>>4), level(batt&0x0F)
	b := &Battery{Case: level(chg & 0x0F), CaseCharging: chg&0x40 != 0}
	c := chg >> 4
	if flip {
		b.Left, b.Right = hi, lo
		b.LeftCharging, b.RightCharging = c&0x2 != 0, c&0x1 != 0
	} else {
		b.Left, b.Right = lo, hi
		b.LeftCharging, b.RightCharging = c&0x1 != 0, c&0x2 != 0
	}
	if b.Left != nil || b.Right != nil || b.Case != nil {
		id.Battery = b
	}
	return id
}

// level turns a battery nibble (0-10 = tens of percent, 15 = not reported)
// into a percentage.
func level(n byte) *int {
	if n > 10 {
		return nil
	}
	p := int(n) * 10
	return &p
}

func uuidText(b []byte) string {
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Kinds lists every kind, for API validation and UI filters.
var Kinds = []string{KindPhone, KindHeadphones, KindWatch, KindTV, KindTracker, KindFindMy, KindBeacon, KindHome, KindOther}

// ValidKind reports a known kind.
func ValidKind(k string) bool { return slices.Contains(Kinds, k) }
