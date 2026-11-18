package presence

import "testing"

func TestPresent(t *testing.T) {
	now := int64(1_000_000_000)
	cases := []struct {
		last int64
		want bool
	}{
		{0, false},                // never heard
		{now, true},               // just now
		{now - GapMS, true},       // exactly at the boundary
		{now - GapMS - 1, false},  // just over
		{now - 10*60*1000, false}, // ten minutes ago
	}
	for _, c := range cases {
		if got := Present(c.last, now); got != c.want {
			t.Errorf("Present(%d) = %v, want %v", now-c.last, got, c.want)
		}
	}
}

func TestBuildSessions(t *testing.T) {
	now := int64(10_000_000)
	// A finished pair, a finished triple, then an ongoing pair near now.
	times := []int64{
		now - 3_000_000,
		now - 2_999_000,
		now - 1_000_000,
		now - 999_000,
		now - 998_000,
		now - 200_000,
		now - 100_000,
	}
	ss := Build(times, now)
	if len(ss) != 3 {
		t.Fatalf("sessions = %d, want 3 (%+v)", len(ss), ss)
	}
	if ss[0].Start != times[0] || ss[0].End != times[1] || ss[0].Sightings != 2 || ss[0].Ongoing {
		t.Fatalf("session 0: %+v", ss[0])
	}
	if ss[1].Start != times[2] || ss[1].End != times[4] || ss[1].Sightings != 3 || ss[1].Ongoing {
		t.Fatalf("session 1: %+v", ss[1])
	}
	if ss[2].Start != times[5] || ss[2].End != times[6] || ss[2].Sightings != 2 || !ss[2].Ongoing {
		t.Fatalf("session 2: %+v", ss[2])
	}
	if ss[2].DurationMS != now-ss[2].Start { // ongoing: counts up to now
		t.Fatalf("ongoing duration: %d", ss[2].DurationMS)
	}

	// A finished session's duration is its last sighting minus the first.
	finished := Build([]int64{now - 600_000, now - 500_000}, now)
	if len(finished) != 1 || finished[0].Ongoing || finished[0].DurationMS != 100_000 {
		t.Fatalf("finished: %+v", finished)
	}
}

func TestBuildEmptyAndDuplicates(t *testing.T) {
	if ss := Build(nil, 1000); len(ss) != 0 {
		t.Fatalf("empty: %+v", ss)
	}
	ss := Build([]int64{100, 100, 100}, 100+10*60*1000) // same ms, stale
	if len(ss) != 1 || ss[0].Sightings != 1 || ss[0].Ongoing {
		t.Fatalf("duplicates: %+v", ss)
	}
}

func TestDwellAndCount(t *testing.T) {
	now := int64(10_000_000)
	ss := []Session{
		{Start: now - 600_000, End: now - 500_000}, // 100 s, fully inside
		{Start: now - 400_000, End: now - 60_000, Ongoing: true},
		{Start: now - 2_000_000, End: now - 1_900_000}, // before `since`
	}
	since := now - 1_000_000
	if got, want := Dwell(ss, since, now), int64(100_000+400_000); got != want {
		t.Fatalf("dwell = %d, want %d", got, want)
	}
	if got := CountSince(ss, since); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
}
