// Package detection turns observations into explainable findings.
//
// Detectors are deliberately simple, threshold- and rule-based, and every
// finding states its evidence, confidence and limitations. Detectors never
// emit the "confirmed" classification; only an analyst can confirm.
package detection

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// Classification levels (docs/detection.md).
const (
	ClassObserved   = "observed"
	ClassSuspicious = "suspicious"
	ClassPotential  = "potential_attack"
)

// Lookup gives detectors read access to current state.
type Lookup interface {
	APsBySSIDHex(hex string) ([]storage.AccessPoint, error)
	GetAP(bssid string) (*storage.AccessPoint, error) // nil if unknown
}

// Context is what a detector sees for one event.
type Context struct {
	Event *events.Event
	// PrevAP is the AP row before this event was applied (nil if the BSSID
	// was unknown or the event has no BSSID). CurAP is the row after.
	PrevAP, CurAP *storage.AccessPoint
	Lookup        Lookup
	Now           time.Time
}

// Finding is a detector result before it gets an ID and timestamp.
type Finding struct {
	Category       string
	Severity       string
	Confidence     float64
	Classification string
	BSSID          string
	Explanation    string
	Limitations    string
	Evidence       []string
}

// Detector is one detection rule.
type Detector interface {
	Name() string
	Observe(c *Context) []Finding
}

// Engine runs all detectors. Safe for use from one goroutine at a time
// (the ingestion pipeline); the mutex guards against accidental misuse.
type Engine struct {
	mu        sync.Mutex
	detectors []Detector
}

// NewEngine returns an engine with the default detectors.
func NewEngine() *Engine {
	return &Engine{detectors: []Detector{
		newDeauthDetector(),
		newSecurityChangeDetector(),
		newRogueAPDetector(),
		newPMFInvalidDetector(),
	}}
}

// Observe runs every detector and returns storable findings.
func (e *Engine) Observe(c *Context) []storage.Finding {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []storage.Finding
	for _, d := range e.detectors {
		for _, f := range d.Observe(c) {
			out = append(out, toStored(f, c))
		}
	}
	return out
}

func toStored(f Finding, c *Context) storage.Finding {
	now := c.Now.UnixMilli()
	sensor := c.Event.SensorID
	var bssid *string
	if f.BSSID != "" {
		b := f.BSSID
		bssid = &b
	}
	ev := f.Evidence
	if len(ev) > 64 {
		ev = ev[len(ev)-64:]
	}
	if ev == nil {
		ev = []string{}
	}
	return storage.Finding{
		ID: newID(), Category: f.Category, Severity: f.Severity, Confidence: f.Confidence,
		Classification: f.Classification, BSSID: bssid, SensorID: &sensor,
		Explanation: f.Explanation, Limitations: f.Limitations, Evidence: ev,
		Status: "open", CreatedAt: now, UpdatedAt: now,
	}
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "f-" + hex.EncodeToString(b[:])
}

// cooldown suppresses repeat findings for the same key.
type cooldown struct {
	period time.Duration
	last   map[string]time.Time
}

func newCooldown(d time.Duration) *cooldown {
	return &cooldown{period: d, last: map[string]time.Time{}}
}

func (c *cooldown) allow(key string, now time.Time) bool {
	if t, ok := c.last[key]; ok && now.Sub(t) < c.period {
		return false
	}
	c.last[key] = now
	return true
}
