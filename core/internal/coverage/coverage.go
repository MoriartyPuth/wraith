// Package coverage reconstructs what each sensor's single radio was doing
// over a time window: Wi-Fi monitoring, Bluetooth (with or without Classic
// inquiry), or no data at all (sensor offline, link down or Core not
// running). A switch-mode sensor is blind to one radio while on the other,
// so this is what tells an operator which absences of events mean something.
//
// Build is pure; the storage layer supplies mode changes (from
// bluetooth.system.status events) and heartbeat gaps.
package coverage

import "sort"

// Segment kinds.
const (
	KindWifi      = "wifi"
	KindBluetooth = "bluetooth"
	KindNoData    = "no_data"
)

// Segment is one interval [From, To) in Unix ms.
type Segment struct {
	From    int64  `json:"from"`
	To      int64  `json:"to"`
	Kind    string `json:"kind"`
	Classic bool   `json:"classic,omitempty"` // Bluetooth with Classic inquiry on (the sensor transmits)
}

// Change is a reported radio state at time At.
type Change struct {
	At      int64
	Mode    string // "wifi" | "bluetooth"
	Classic bool
}

// Gap is an interval with no heartbeat [From, To).
type Gap struct{ From, To int64 }

// Build covers [start, end) exactly, without overlaps: the radio state from
// initial and the changes, overridden by no-data gaps. Adjacent equal
// segments are merged. Changes and gaps may be unsorted and may extend
// outside the window.
func Build(start, end int64, initial Change, changes []Change, gaps []Gap) []Segment {
	if end <= start {
		return nil
	}
	cur := Segment{Kind: kindOf(initial.Mode), Classic: initial.Mode == KindBluetooth && initial.Classic}
	// Boundaries where the state may change.
	points := map[int64]struct{}{start: {}}
	for _, c := range changes {
		if c.At > start && c.At < end {
			points[c.At] = struct{}{}
		}
	}
	for _, g := range gaps {
		for _, t := range []int64{g.From, g.To} {
			if t > start && t < end {
				points[t] = struct{}{}
			}
		}
	}
	ts := make([]int64, 0, len(points))
	for t := range points {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	changes = append([]Change(nil), changes...) // do not reorder the caller's slice
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].At < changes[j].At })

	var out []Segment
	ci := 0
	for i, t := range ts {
		to := end
		if i+1 < len(ts) {
			to = ts[i+1]
		}
		// Apply every change at or before t (the latest wins).
		for ci < len(changes) && changes[ci].At <= t {
			c := changes[ci]
			cur = Segment{Kind: kindOf(c.Mode), Classic: c.Mode == KindBluetooth && c.Classic}
			ci++
		}
		s := Segment{From: t, To: to, Kind: cur.Kind, Classic: cur.Classic}
		for _, g := range gaps {
			if g.From <= t && t < g.To {
				s.Kind, s.Classic = KindNoData, false
				break
			}
		}
		if n := len(out); n > 0 && out[n-1].Kind == s.Kind && out[n-1].Classic == s.Classic {
			out[n-1].To = s.To
			continue
		}
		out = append(out, s)
	}
	return out
}

// Totals sums the time per kind (Classic counted inside Bluetooth and on its own).
func Totals(segs []Segment) map[string]int64 {
	t := map[string]int64{KindWifi: 0, KindBluetooth: 0, KindNoData: 0, "classic": 0}
	for _, s := range segs {
		t[s.Kind] += s.To - s.From
		if s.Classic {
			t["classic"] += s.To - s.From
		}
	}
	return t
}

func kindOf(mode string) string {
	if mode == KindBluetooth {
		return KindBluetooth
	}
	return KindWifi
}
