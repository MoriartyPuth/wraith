// Package ingest applies validated events to storage, runs detection and
// publishes live updates. It is the single writer for observation data.
//
// Per event, in one transaction: dedup (in-memory LRU + DB uniqueness),
// sequence-gap and restart accounting, event row, sensor state, AP inventory,
// RSSI history, detection findings. Live updates are published only after
// the transaction commits, so the stream never shows uncommitted state.
package ingest

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/detection"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/stream"
	"github.com/MoriartyPuth/wraith/core/internal/transport"
)

// Publisher receives committed updates (implemented by stream.Hub).
type Publisher interface {
	Publish(kind string, data any)
}

// FindingDedupWindow is how long a stored finding suppresses an equivalent
// new one (same category + BSSID). It spans typical restart gaps so the same
// ongoing condition is recorded once, not once per Core run.
const FindingDedupWindow = 6 * time.Hour

// Pipeline is the ingestion pipeline.
type Pipeline struct {
	store *storage.Store
	det   *detection.Engine
	pub   Publisher
	log   *slog.Logger
	seqs  *transport.SeqTracker
	now   func() time.Time
	start time.Time

	Ingested, Duplicates, Gaps, Restarts, Failures, Deduped atomic.Uint64
}

// New creates a pipeline. pub may be nil.
func New(store *storage.Store, det *detection.Engine, pub Publisher, log *slog.Logger) *Pipeline {
	return &Pipeline{store: store, det: det, pub: pub, log: log, seqs: transport.NewSeqTracker(4096), now: time.Now, start: time.Now()}
}

// Run consumes events until in is closed or ctx is done.
func (p *Pipeline) Run(ctx context.Context, in <-chan capture.WirelessEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			if err := p.Handle(ctx, &ev); err != nil && !errors.Is(err, context.Canceled) {
				p.Failures.Add(1)
				p.log.Error("ingest failed", "event_id", ev.EventID, "sensor", ev.SensorID, "err", err)
			}
		}
	}
}

// Rejected records an input a provider could not validate.
func (p *Pipeline) Rejected(r capture.Rejection) {
	p.log.Warn("event rejected", "provider", r.Provider, "sensor", r.SensorID, "reason", r.Reason, "detail", r.Detail)
	if r.SensorID == "" {
		return // counted in the provider's stats only
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.store.IncSensorCounter(ctx, r.SensorID, events.SourceNano, storage.CounterRejected, 1, p.now().UnixMilli()); err != nil {
		p.log.Error("count rejection", "err", err)
	}
}

// ProviderStatus publishes a provider state change.
func (p *Pipeline) ProviderStatus(st capture.ProviderStatus) {
	p.log.Info("capture provider", "provider", st.Name, "state", st.State, "msg", st.Message)
	p.publish(stream.KindProvider, st)
}

func (p *Pipeline) publish(kind string, v any) {
	if p.pub != nil {
		p.pub.Publish(kind, v)
	}
}

type lookup struct {
	ctx context.Context
	tx  *storage.Tx
}

func (l lookup) APsBySSIDHex(hex string) ([]storage.AccessPoint, error) {
	return l.tx.APsBySSIDHex(l.ctx, hex)
}
func (l lookup) GetAP(bssid string) (*storage.AccessPoint, error) {
	a, err := l.tx.GetAP(l.ctx, bssid)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	return &a, err
}

// Handle ingests one validated event.
func (p *Pipeline) Handle(ctx context.Context, ev *events.Event) error {
	now := p.now()
	if ev.ReceivedAt.IsZero() {
		ev.ReceivedAt = now
	}
	nowMS := ev.ReceivedAt.UnixMilli()

	v := p.seqs.Track(ev.SensorID, ev.EventID, ev.Seq)
	if v.Duplicate {
		p.Duplicates.Add(1)
		return p.store.IncSensorCounter(ctx, ev.SensorID, ev.Source, storage.CounterDuplicate, 1, nowMS)
	}
	if v.Restart {
		p.Restarts.Add(1)
		p.log.Info("sensor restarted", "sensor", ev.SensorID, "boot_id", bootID(ev.EventID))
	}

	var (
		stored   storage.StoredEvent
		ap       *storage.AccessPoint
		findings []storage.Finding
		dup      bool
	)
	err := p.store.WithTx(ctx, func(tx *storage.Tx) error {
		if err := tx.TouchSensor(ctx, ev.SensorID, ev.Source, nowMS); err != nil {
			return err
		}
		stored = storage.StoredEvent{EventID: ev.EventID, SensorID: ev.SensorID, Source: ev.Source,
			EventType: ev.Type, Channel: ev.Channel, SensorTS: ev.Timestamp, ReceivedAt: nowMS, Payload: ev.Raw}
		if ev.BSSID != "" {
			b := ev.BSSID
			stored.BSSID = &b
		}
		row, inserted, err := tx.InsertEvent(ctx, stored)
		if err != nil {
			return err
		}
		if !inserted { // seen before this Core run (DB uniqueness)
			dup = true
			return nil
		}
		stored.ID = row

		if err := p.applySensor(ctx, tx, ev, nowMS); err != nil {
			return err
		}
		var prev *storage.AccessPoint
		if ev.BSSID != "" {
			lk := lookup{ctx, tx}
			if prev, err = lk.GetAP(ev.BSSID); err != nil {
				return err
			}
			changed, err := p.applyAP(ctx, tx, ev, row, nowMS)
			if err != nil {
				return err
			}
			if changed {
				if ap, err = lk.GetAP(ev.BSSID); err != nil {
					return err
				}
			} else {
				ap = prev
			}
		}
		raw := p.det.Observe(&detection.Context{Event: ev, PrevAP: prev, CurAP: ap, Lookup: lookup{ctx, tx},
			Now: ev.ReceivedAt, CoreUptime: p.now().Sub(p.start)})
		// Suppress findings equivalent to one already stored recently, so a
		// Core restart (which clears detectors' in-memory dedup) does not
		// create duplicates for the same ongoing condition.
		dedupSince := ev.ReceivedAt.Add(-FindingDedupWindow).UnixMilli()
		findings = findings[:0]
		for _, f := range raw {
			bssid := ""
			if f.BSSID != nil {
				bssid = *f.BSSID
			}
			exists, err := tx.FindingExistsSince(ctx, f.Category, bssid, dedupSince)
			if err != nil {
				return err
			}
			if exists {
				p.Deduped.Add(1)
				continue
			}
			if err := tx.InsertFinding(ctx, f); err != nil {
				return err
			}
			findings = append(findings, f)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if dup {
		p.Duplicates.Add(1)
		return p.store.IncSensorCounter(ctx, ev.SensorID, ev.Source, storage.CounterDuplicate, 1, nowMS)
	}
	if v.Gap > 0 {
		p.Gaps.Add(v.Gap)
		p.log.Warn("sequence gap: events lost between sensor and core", "sensor", ev.SensorID, "missing", v.Gap)
		if err := p.store.IncSensorCounter(ctx, ev.SensorID, ev.Source, storage.CounterGap, int64(v.Gap), nowMS); err != nil {
			return err
		}
	}
	p.Ingested.Add(1)

	p.publish(stream.KindEvent, stored)
	if ap != nil {
		p.publish(stream.KindAP, ap)
	}
	for _, f := range findings {
		p.log.Info("finding", "id", f.ID, "category", f.Category, "severity", f.Severity, "class", f.Classification, "bssid", deref(f.BSSID))
		p.publish(stream.KindFinding, f)
	}
	if ev.Type == events.TypeHeartbeat || ev.Type == events.TypeSensorStatus || v.Restart {
		if s, err := p.store.GetSensor(ctx, ev.SensorID); err == nil {
			p.publish(stream.KindSensor, s)
		}
	}
	return nil
}

func (p *Pipeline) applySensor(ctx context.Context, tx *storage.Tx, ev *events.Event, now int64) error {
	switch ev.Type {
	case events.TypeHeartbeat:
		var m events.HeartbeatMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return err
		}
		return tx.SensorHeartbeat(ctx, ev.SensorID, now, m.Channel, bootID(ev.EventID))
	case events.TypeSensorStatus:
		var m events.StatusMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return err
		}
		return tx.SensorStatus(ctx, ev.SensorID, m.State, m.FirmwareVersion, m.Board, m.Capabilities, bootID(ev.EventID))
	}
	return nil
}

// applyAP merges AP-related events into the inventory. Returns whether the
// AP row was touched.
func (p *Pipeline) applyAP(ctx context.Context, tx *storage.Tx, ev *events.Event, row, now int64) (bool, error) {
	u := storage.APUpdate{BSSID: ev.BSSID, Seen: now, Channel: ev.Channel, Sensor: ev.SensorID}
	switch ev.Type {
	case events.TypeAPObserved:
		var m events.APMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return false, err
		}
		u.SSID, u.SSIDHex, u.RSSI = &m.SSID, &m.SSIDHex, &m.RSSI
		if m.AuthMode != "" {
			u.AuthMode = &m.AuthMode
		}
	case events.TypeBeaconObserved:
		var m events.BeaconMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return false, err
		}
		u.SSID, u.SSIDHex, u.RSSI = &m.SSID, &m.SSIDHex, &m.RSSI
		u.SetRSN = true
		if m.RSN != nil {
			u.RSN = rawField(ev.Metadata, "rsn")
		}
	case events.TypePMFObserved:
		var m events.PMFMeta
		if err := ev.DecodeMeta(&m); err != nil {
			return false, err
		}
		u.PMF = &m.PMF
	default:
		return false, nil // deauth/disassoc reference a BSSID but are not AP observations
	}
	if err := tx.UpsertAP(ctx, u); err != nil {
		return false, err
	}
	if u.RSSI != nil {
		if err := tx.InsertObservation(ctx, row, ev.BSSID, ev.SensorID, ev.Channel, u.RSSI, now); err != nil {
			return false, err
		}
	}
	return true, nil
}

func bootID(eventID string) string {
	if i := strings.LastIndexByte(eventID, '-'); i > 0 {
		return eventID[:i]
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
