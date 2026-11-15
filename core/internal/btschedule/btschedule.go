// Package btschedule plans unattended Bluetooth sessions. A switch-mode
// sensor has one radio: a scheduled session pauses Wi-Fi monitoring for its
// duration, so this is a deliberate trade, configured by the operator and
// driven by Core (it sends the same MODE commands as the dashboard button).
//
// The planner here is pure; the Core daemon wires it to the real clock,
// command path and sensor state (driver.go).
package btschedule

import "fmt"

// Schedule is the stored configuration (one global schedule).
type Schedule struct {
	Enabled         bool `json:"enabled"`
	EveryMinutes    int  `json:"every_minutes"`    // minutes between session starts
	DurationSeconds int  `json:"duration_seconds"` // how long each session lasts
	Classic         bool `json:"classic"`          // include Classic inquiry (transmits)
}

// Limits for a valid schedule. Duration stays below the firmware's 15-minute
// session cap and must fit inside the interval with room for the switch back.
const (
	MinEveryMinutes    = 1
	MaxEveryMinutes    = 1440 // once a day
	MinDurationSeconds = 10
	MaxDurationSeconds = 600 // 10 min; firmware caps a session at 15 min
)

// Validate returns why the schedule is not usable, or nil. A disabled
// schedule is always valid (the fields are ignored until enabled, but we
// still bound them so a saved-then-enabled schedule is sane).
func (s Schedule) Validate() error {
	if s.EveryMinutes < MinEveryMinutes || s.EveryMinutes > MaxEveryMinutes {
		return fmt.Errorf("every_minutes must be %d-%d", MinEveryMinutes, MaxEveryMinutes)
	}
	if s.DurationSeconds < MinDurationSeconds || s.DurationSeconds > MaxDurationSeconds {
		return fmt.Errorf("duration_seconds must be %d-%d", MinDurationSeconds, MaxDurationSeconds)
	}
	if int64(s.DurationSeconds) >= int64(s.EveryMinutes)*60 {
		return fmt.Errorf("duration_seconds must be shorter than the interval (%d s)", s.EveryMinutes*60)
	}
	return nil
}

// Default is the schedule a fresh install reports: off, a 5-minute BLE
// session every hour.
func Default() Schedule {
	return Schedule{Enabled: false, EveryMinutes: 60, DurationSeconds: 300, Classic: false}
}

func (s Schedule) intervalMS() int64 { return int64(s.EveryMinutes) * 60_000 }

// NextStart is the first aligned session start strictly after `afterMS`
// (Unix ms). Starts are aligned to the interval from the Unix epoch, so a
// 60-minute schedule fires on the hour and a 15-minute one at :00/:15/:30/:45.
func (s Schedule) NextStart(afterMS int64) int64 {
	iv := s.intervalMS()
	if iv <= 0 {
		return afterMS
	}
	// floor to the interval, then step once — strictly after even on a boundary.
	floor := afterMS - mod(afterMS, iv)
	return floor + iv
}

// mod is a floored modulo so alignment is correct for negative times too.
func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}
