// Package presence derives watchlist presence from stored Bluetooth
// sightings. It is pure computation over timestamps plus a periodic evaluator
// that turns presence transitions into findings. No radio or firmware
// involvement: it works from the bt_observations Core already stores.
package presence

const (
	// GapMS: silence longer than this ends a presence session. It is also
	// the window after which the evaluator reports a departure.
	GapMS = 5 * 60 * 1000
	// EvalInterval is how often the evaluator checks watched devices.
	EvalIntervalSeconds = 60
)

// Present reports whether a device last heard at `last` (Unix ms) still
// counts as present at `now`. last == 0 means never heard.
func Present(last, now int64) bool {
	return last > 0 && now-last <= GapMS
}

// Session is one contiguous period of sightings (gaps <= GapMS).
type Session struct {
	Start      int64 `json:"start"`
	End        int64 `json:"end"` // last sighting (not the gap end)
	DurationMS int64 `json:"duration_ms"`
	Sightings  int   `json:"sightings"`
	Ongoing    bool  `json:"ongoing"`
}

// Build groups ascending sighting times into sessions. An ongoing session
// (last sighting within GapMS of now) has DurationMS up to now, so it grows
// while the device stays around. Duplicate timestamps are counted once.
func Build(times []int64, now int64) []Session {
	out := []Session{}
	if len(times) == 0 {
		return out
	}
	start, prev, n := times[0], times[0], 1
	flush := func(ongoing bool) {
		end := prev
		dur := end - start
		if ongoing && now > end {
			dur = now - start
		}
		if dur < 0 {
			dur = 0
		}
		out = append(out, Session{Start: start, End: end, DurationMS: dur, Sightings: n, Ongoing: ongoing})
	}
	for _, t := range times[1:] {
		if t == prev {
			continue // same millisecond, do not double-count
		}
		if t-prev > GapMS {
			flush(false)
			start, n = t, 1
		} else {
			n++
		}
		prev = t
	}
	flush(Present(prev, now))
	return out
}

// Dwell sums session durations for sessions that (partly) fall within
// [since, now]. Ongoing sessions count only the part after `since`.
func Dwell(sessions []Session, since, now int64) int64 {
	var total int64
	for _, s := range sessions {
		start := s.Start
		if start < since {
			start = since
		}
		end := s.End
		if s.Ongoing && now > end {
			end = now
		}
		if end > start {
			total += end - start
		}
	}
	return total
}

// CountSince counts sessions that started at or after `since`.
func CountSince(sessions []Session, since int64) int {
	n := 0
	for _, s := range sessions {
		if s.Start >= since {
			n++
		}
	}
	return n
}
