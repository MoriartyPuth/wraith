package btschedule

import "testing"

func TestValidate(t *testing.T) {
	ok := Schedule{Enabled: true, EveryMinutes: 60, DurationSeconds: 300}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		s    Schedule
	}{
		{"interval too small", Schedule{EveryMinutes: 0, DurationSeconds: 10}},
		{"interval too big", Schedule{EveryMinutes: 1441, DurationSeconds: 10}},
		{"duration too small", Schedule{EveryMinutes: 60, DurationSeconds: 5}},
		{"duration too big", Schedule{EveryMinutes: 60, DurationSeconds: 601}},
		{"duration >= interval", Schedule{EveryMinutes: 5, DurationSeconds: 300}},
	} {
		if err := tc.s.Validate(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestNextStartAlignsToInterval(t *testing.T) {
	s := Schedule{EveryMinutes: 15} // 900_000 ms
	// 10:07 → 10:15; exactly 10:15 → 10:30 (strictly after).
	base := int64(10*3600+7*60) * 1000
	if got := s.NextStart(base); got != int64(10*3600+15*60)*1000 {
		t.Fatalf("10:07 → %d", got)
	}
	onBoundary := int64(10*3600+15*60) * 1000
	if got := s.NextStart(onBoundary); got != int64(10*3600+30*60)*1000 {
		t.Fatalf("boundary → %d", got)
	}
	hourly := Schedule{EveryMinutes: 60}
	if got := hourly.NextStart(int64(9*3600+1)*1000) % 3_600_000; got != 0 {
		t.Fatalf("hourly not aligned to the hour: %d", got)
	}
}

// drive a planner through a minute-by-minute timeline (ms).
func TestPlannerSessionsAndOperatorOverride(t *testing.T) {
	var p Planner
	min := func(m int64) int64 { return m * 60_000 }
	// 10-minute interval (fits a 300 s session), BLE only. Align at t=0.
	p.SetConfig(Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 300, Classic: false}, min(0)+1)
	if due, ok := p.NextDue(); !ok || due != min(10) {
		t.Fatalf("next due %d", due)
	}
	// Nothing until the first aligned start at t=10m.
	if a := p.Tick(min(9), false); a != ActNone {
		t.Fatalf("early tick: %v", a)
	}
	if a := p.Tick(min(10), false); a != ActStartBLE {
		t.Fatalf("start: %v", a)
	}
	if in, end := p.InSession(); !in || end != min(10)+300_000 {
		t.Fatalf("session end %d", end)
	}
	// Mid-session: no action until the duration elapses.
	if a := p.Tick(min(10)+200_000, true); a != ActNone {
		t.Fatalf("mid session: %v", a)
	}
	if a := p.Tick(min(10)+300_000, true); a != ActStop {
		t.Fatalf("stop: %v", a)
	}
	if in, _ := p.InSession(); in {
		t.Fatal("still in session after stop")
	}
	// Next start at t=20m, but the operator is already on Bluetooth: skip it,
	// do not take over, and do not later force Wi-Fi.
	if a := p.Tick(min(20), true); a != ActNone {
		t.Fatalf("operator override should skip: %v", a)
	}
	if in, _ := p.InSession(); in {
		t.Fatal("must not manage an operator session")
	}
	// t=30m, radio back on Wi-Fi: a scheduled session starts again.
	if a := p.Tick(min(30), false); a != ActStartBLE {
		t.Fatalf("resume: %v", a)
	}
}

func TestPlannerClassicAndDisable(t *testing.T) {
	var p Planner
	p.SetConfig(Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 60, Classic: true}, 1)
	if a := p.Tick(600_000, false); a != ActStartClassic {
		t.Fatalf("classic start: %v", a)
	}
	// Disabling mid-session ends it at once.
	if a := p.SetConfig(Schedule{Enabled: false, EveryMinutes: 10, DurationSeconds: 60}, 605_000); a != ActStop {
		t.Fatalf("disable should stop the running session: %v", a)
	}
	if _, ok := p.NextDue(); ok {
		t.Fatal("disabled schedule has no next due")
	}
	if a := p.Tick(1_200_000, false); a != ActNone {
		t.Fatalf("disabled must be idle: %v", a)
	}
	// Disabling when not in a session does nothing.
	if a := p.SetConfig(Schedule{Enabled: false, EveryMinutes: 10, DurationSeconds: 60}, 2_000_000); a != ActNone {
		t.Fatalf("disable idle: %v", a)
	}
}
