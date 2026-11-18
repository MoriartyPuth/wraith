package presence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/stream"
)

// Publisher receives findings (implemented by stream.Hub).
type Publisher interface {
	Publish(kind string, data any)
}

// Evaluator turns watchlist presence transitions into findings. It is the
// only writer of arrival/departure findings; state is persisted in
// bt_watchlist so a Core restart neither loses nor repeats a transition.
type Evaluator struct {
	store *storage.Store
	log   *slog.Logger
	pub   Publisher
	now   func() time.Time
}

func New(store *storage.Store, log *slog.Logger, pub Publisher) *Evaluator {
	return &Evaluator{store: store, log: log, pub: pub, now: time.Now}
}

// Run evaluates immediately, then every interval until ctx is done.
func (e *Evaluator) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = EvalIntervalSeconds * time.Second
	}
	e.evaluate(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.evaluate(ctx)
		}
	}
}

func (e *Evaluator) evaluate(ctx context.Context) {
	findings, err := e.Evaluate(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		e.log.Warn("presence evaluation failed", "err", err)
	}
	for _, f := range findings {
		e.log.Info("watchlist presence", "id", f.ID, "category", f.Category, "device", deref(f.BTDevice))
		if e.pub != nil {
			e.pub.Publish(stream.KindFinding, f)
		}
	}
}

// Evaluate checks every watched device once and returns any new findings.
// Exported for tests.
func (e *Evaluator) Evaluate(ctx context.Context) ([]storage.Finding, error) {
	watches, err := e.store.ListWatchlist(ctx)
	if err != nil {
		return nil, err
	}
	now := e.now().UnixMilli()
	var out []storage.Finding
	for _, w := range watches {
		f, err := e.evalOne(ctx, w, now)
		if err != nil {
			return out, fmt.Errorf("%s: %w", w.ID, err)
		}
		if f != nil {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (e *Evaluator) evalOne(ctx context.Context, w storage.WatchEntry, now int64) (*storage.Finding, error) {
	dev, err := e.store.GetBTDevice(ctx, w.Transport, w.Address, w.AddressType)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	var last int64
	name := w.Address
	var sensor *string
	if err == nil {
		last = dev.LastSeen
		if dev.Name != nil && *dev.Name != "" {
			name = *dev.Name
		}
		sensor = dev.LastSensor
	}
	if w.Label != "" {
		name = w.Label
	}
	present := Present(last, now)
	if present == w.Present {
		return nil, nil
	}
	if err := e.store.SetWatchPresence(ctx, w.Transport, w.Address, w.AddressType, present, now, now); err != nil {
		return nil, err
	}
	f := storage.Finding{
		ID: newID(), Classification: "observed", Status: "open",
		BTDevice: &w.ID, SensorID: sensor, Evidence: []string{},
		CreatedAt: now, UpdatedAt: now,
	}
	if present {
		f.Category = "bt_watch_arrived"
		f.Severity = "info"
		f.Confidence = 1
		f.Explanation = fmt.Sprintf("Watched device %q was heard again.", name)
		f.Limitations = "Presence is inferred from sightings: a silent device may still be present, and BLE random addresses rotate."
	} else {
		f.Category = "bt_watch_left"
		f.Severity = "low"
		f.Confidence = 0.8
		f.Explanation = fmt.Sprintf("Watched device %q has not been heard for 5 minutes.", name)
		f.Limitations = "Absence of sightings is not proof of absence: the sensor scans one radio mode at a time and a quiet device may have moved out of range or changed address."
	}
	if err := e.store.WithTx(ctx, func(tx *storage.Tx) error {
		return tx.InsertFinding(ctx, f)
	}); err != nil {
		return nil, err
	}
	return &f, nil
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "f-" + hex.EncodeToString(b[:])
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
