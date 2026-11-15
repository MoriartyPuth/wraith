package btschedule

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu sync.Mutex
	m  map[string]string
}

func (f *fakeStore) GetConfig(_ context.Context, k string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fakeStore) SetConfig(_ context.Context, k, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[k] = v
	return nil
}

type fakeEnv struct {
	sent      []string
	bt        bool
	canSwitch bool
	classicOK bool
	sendErr   error
}

func (e *fakeEnv) SendCommand(line string) error {
	if e.sendErr != nil {
		return e.sendErr
	}
	e.sent = append(e.sent, line)
	return nil
}
func (e *fakeEnv) RadioBluetooth(context.Context) bool { return e.bt }
func (e *fakeEnv) Ready(context.Context) (bool, bool)  { return e.canSwitch, e.classicOK }

func newRunner(t *testing.T, env *fakeEnv, clk *int64) (*Runner, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	now := func() time.Time { return time.UnixMilli(*clk) }
	r := New(context.Background(), store, env, slog.New(slog.NewTextHandler(io.Discard, nil)), now)
	return r, store
}

func TestRunnerSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	var clk int64 = 1_000_000
	env := &fakeEnv{canSwitch: true, classicOK: true}
	r, store := newRunner(t, env, &clk)

	// Enable: 10-minute interval, 60 s BLE sessions.
	if err := r.Set(ctx, Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 60, Classic: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.m[ConfigKey]; !ok {
		t.Fatal("schedule not persisted")
	}
	_, nextRun, nextOK, _ := r.Get()
	if !nextOK || nextRun%600_000 != 0 {
		t.Fatalf("next run not aligned: %d", nextRun)
	}

	clk = nextRun // first aligned start
	r.Step(ctx)
	if len(env.sent) != 1 || env.sent[0] != "MODE BLUETOOTH" {
		t.Fatalf("start: %v", env.sent)
	}
	if _, _, _, in := r.Get(); !in {
		t.Fatal("should be in a scheduled session")
	}
	env.bt = true // the sensor is now in Bluetooth mode
	clk += 30_000
	r.Step(ctx)
	if len(env.sent) != 1 {
		t.Fatalf("mid session sent %v", env.sent)
	}
	clk += 30_000 // duration elapsed
	r.Step(ctx)
	if len(env.sent) != 2 || env.sent[1] != "MODE WIFI" {
		t.Fatalf("stop: %v", env.sent)
	}
	if _, _, _, in := r.Get(); in {
		t.Fatal("session should have ended")
	}
}

func TestRunnerClassicAndFallback(t *testing.T) {
	ctx := context.Background()
	var clk int64 = 0
	env := &fakeEnv{canSwitch: true, classicOK: true}
	r, _ := newRunner(t, env, &clk)
	r.Set(ctx, Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 60, Classic: true})
	_, next, _, _ := r.Get()
	clk = next
	r.Step(ctx)
	if env.sent[len(env.sent)-1] != "MODE BLUETOOTH CLASSIC" {
		t.Fatalf("classic start: %v", env.sent)
	}
	// Firmware without Classic support: next session falls back to BLE.
	clk += 60_000
	r.Step(ctx) // stop
	env.classicOK = false
	env.bt = false
	_, next, _, _ = r.Get()
	clk = next
	r.Step(ctx)
	if env.sent[len(env.sent)-1] != "MODE BLUETOOTH" {
		t.Fatalf("fallback to BLE: %v", env.sent)
	}
}

func TestRunnerSkipsWhenNotReadyNoStrayStop(t *testing.T) {
	ctx := context.Background()
	var clk int64 = 0
	env := &fakeEnv{canSwitch: false} // no switch-capable sensor connected
	r, _ := newRunner(t, env, &clk)
	r.Set(ctx, Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 60})
	_, next, _, _ := r.Get()
	clk = next
	r.Step(ctx)
	if len(env.sent) != 0 {
		t.Fatalf("nothing should be sent when not ready: %v", env.sent)
	}
	// The planner must not think a session is running (no later MODE WIFI).
	clk += 120_000
	r.Step(ctx)
	if len(env.sent) != 0 {
		t.Fatalf("a skipped start must not produce a stray stop: %v", env.sent)
	}
}

func TestRunnerOperatorOverrideNotTouched(t *testing.T) {
	ctx := context.Background()
	var clk int64 = 0
	env := &fakeEnv{canSwitch: true, bt: true} // operator already on Bluetooth
	r, _ := newRunner(t, env, &clk)
	r.Set(ctx, Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 60})
	_, next, _, _ := r.Get()
	clk = next
	r.Step(ctx)
	clk += 300_000
	r.Step(ctx)
	if len(env.sent) != 0 {
		t.Fatalf("a manual session must be left alone: %v", env.sent)
	}
}

func TestRunnerDisableEndsSession(t *testing.T) {
	ctx := context.Background()
	var clk int64 = 0
	env := &fakeEnv{canSwitch: true}
	r, _ := newRunner(t, env, &clk)
	r.Set(ctx, Schedule{Enabled: true, EveryMinutes: 10, DurationSeconds: 300})
	_, next, _, _ := r.Get()
	clk = next
	r.Step(ctx)
	env.bt = true
	// Turn the schedule off mid-session: it must end the session now.
	clk += 10_000
	if err := r.Set(ctx, Schedule{Enabled: false, EveryMinutes: 10, DurationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if env.sent[len(env.sent)-1] != "MODE WIFI" {
		t.Fatalf("disable should stop the session: %v", env.sent)
	}
}

func TestRunnerLoadsStoredConfig(t *testing.T) {
	store := &fakeStore{m: map[string]string{ConfigKey: `{"enabled":true,"every_minutes":30,"duration_seconds":120,"classic":true}`}}
	var clk int64 = 0
	now := func() time.Time { return time.UnixMilli(clk) }
	r := New(context.Background(), store, &fakeEnv{canSwitch: true, classicOK: true},
		slog.New(slog.NewTextHandler(io.Discard, nil)), now)
	cfg, _, ok, _ := r.Get()
	if !ok || !cfg.Enabled || cfg.EveryMinutes != 30 || !cfg.Classic {
		t.Fatalf("stored config not loaded: %+v", cfg)
	}
	// An invalid stored blob falls back to the default (disabled).
	bad := &fakeStore{m: map[string]string{ConfigKey: `{"enabled":true,"every_minutes":0,"duration_seconds":1}`}}
	r2 := New(context.Background(), bad, &fakeEnv{}, slog.New(slog.NewTextHandler(io.Discard, nil)), now)
	if cfg, _, _, _ := r2.Get(); cfg.Enabled {
		t.Fatal("invalid stored schedule should fall back to disabled default")
	}
}
