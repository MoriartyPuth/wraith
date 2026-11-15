package btschedule

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// ConfigKey is the app_config key the schedule is stored under.
const ConfigKey = "bt_schedule"

// tick cadence: how often the runner evaluates the plan. Session boundaries
// land within this of their aligned time.
const tickInterval = 2 * time.Second

// Store is the subset of storage the runner needs.
type Store interface {
	GetConfig(ctx context.Context, key string) (string, bool, error)
	SetConfig(ctx context.Context, key, value string) error
}

// Sensor state and command sending (the capture manager implements Commander;
// RadioBluetooth reports whether any sensor's radio is currently in Bluetooth
// mode, and Ready whether a scheduled switch can be delivered).
type Env interface {
	SendCommand(line string) error
	RadioBluetooth(ctx context.Context) bool
	// Ready reports whether a sensor that can switch mode is connected, and
	// whether Classic inquiry is supported (false falls back to BLE).
	Ready(ctx context.Context) (canSwitch, classicOK bool)
}

// Runner drives one Planner from the real clock, config store and sensor
// state. Its config is shared (the API reads and writes it).
type Runner struct {
	store Store
	env   Env
	log   *slog.Logger
	now   func() time.Time

	mu      sync.Mutex
	planner Planner
	loaded  Schedule
}

// New loads the stored schedule (or the default) and returns a Runner.
func New(ctx context.Context, store Store, env Env, log *slog.Logger, now func() time.Time) *Runner {
	r := &Runner{store: store, env: env, log: log, now: now}
	cfg := Default()
	if v, ok, err := store.GetConfig(ctx, ConfigKey); err == nil && ok {
		var stored Schedule
		if json.Unmarshal([]byte(v), &stored) == nil && stored.Validate() == nil {
			cfg = stored
		} else {
			log.Warn("stored Bluetooth schedule is invalid; using the default")
		}
	}
	r.loaded = cfg
	r.planner.SetConfig(cfg, now().UnixMilli())
	return r
}

// Get returns the current schedule and, when enabled, the next scheduled
// start (Unix ms) and whether a scheduled session is running now.
func (r *Runner) Get() (cfg Schedule, nextRun int64, nextOK, inSession bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg = r.planner.Config()
	nextRun, nextOK = r.planner.NextDue()
	inSession, _ = r.planner.InSession()
	return
}

// Set validates, persists and applies a new schedule. A scheduled session in
// progress is ended cleanly if the change turns the schedule off or alters it.
func (r *Runner) Set(ctx context.Context, cfg Schedule) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	blob, _ := json.Marshal(cfg)
	if err := r.store.SetConfig(ctx, ConfigKey, string(blob)); err != nil {
		return err
	}
	r.mu.Lock()
	act := r.planner.SetConfig(cfg, r.now().UnixMilli())
	r.loaded = cfg
	r.mu.Unlock()
	r.exec(ctx, act)
	r.log.Info("bluetooth schedule updated", "enabled", cfg.Enabled, "every_minutes", cfg.EveryMinutes,
		"duration_seconds", cfg.DurationSeconds, "classic", cfg.Classic)
	return nil
}

// Run evaluates the plan every tick until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Step(ctx)
		}
	}
}

// Step evaluates the plan once (Run calls it each tick; exported for tests).
func (r *Runner) Step(ctx context.Context) {
	r.mu.Lock()
	act := r.planner.Tick(r.now().UnixMilli(), r.env.RadioBluetooth(ctx))
	r.mu.Unlock()
	r.exec(ctx, act)
}

func (r *Runner) exec(ctx context.Context, act Action) {
	if act == ActNone {
		return
	}
	line := "MODE WIFI"
	if act == ActStartBLE || act == ActStartClassic {
		canSwitch, classicOK := r.env.Ready(ctx)
		if !canSwitch {
			r.log.Warn("scheduled Bluetooth session skipped: no switch-capable sensor is connected")
			r.cancelSession()
			return
		}
		line = "MODE BLUETOOTH"
		if act == ActStartClassic {
			if classicOK {
				line = "MODE BLUETOOTH CLASSIC"
			} else {
				r.log.Warn("scheduled session: Classic inquiry not supported; using BLE only")
			}
		}
	}
	if err := r.env.SendCommand(line); err != nil {
		r.log.Warn("scheduled Bluetooth command failed", "command", line, "err", err)
		if act != ActStop {
			r.cancelSession() // a start that did not go out is not a running session
		}
		return
	}
	r.log.Info("scheduled Bluetooth action", "action", act.String(), "command", line)
}

// cancelSession clears a session the planner opened but the driver could not
// start, so it will not later send a stray MODE WIFI.
func (r *Runner) cancelSession() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in, _ := r.planner.InSession(); in {
		r.planner.SetConfig(r.planner.Config(), r.now().UnixMilli())
	}
}
