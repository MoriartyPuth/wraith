// Package capture defines the provider abstraction that decouples ingestion
// and detection from the capture source. WRAITH's only source is the ESP32
// serial provider; the interface keeps further sensors pluggable.
package capture

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
)

// WirelessEvent is the normalized event every provider emits.
type WirelessEvent = events.Event

// State of a provider.
type State string

const (
	StateUnavailable State = "unavailable" // prerequisites missing (tool, device, config)
	StateStopped     State = "stopped"
	StateStarting    State = "starting"
	StateRunning     State = "running"
	StateDegraded    State = "degraded" // running, but the link is corrupting data
	StateError       State = "error"
)

// ProviderStatus is a point-in-time provider status.
type ProviderStatus struct {
	Name    string    `json:"name"`
	State   State     `json:"state"`
	Message string    `json:"message"`
	Since   time.Time `json:"since"`
	// Implemented is false while the provider is a stub.
	Implemented bool `json:"implemented"`
	// Stats are provider-specific counters (lines read, rejections, ...).
	Stats map[string]uint64 `json:"stats,omitempty"`
}

// Rejection describes an input the provider could not turn into an event.
// SensorID is best-effort (empty if the line was too broken to tell).
type Rejection struct {
	Provider string
	SensorID string
	Reason   string
	Detail   string
}

// ErrNotImplemented is returned by providers whose capture path does not exist yet.
var ErrNotImplemented = errors.New("capture provider not implemented yet")

// ErrUnavailable is returned by Start when prerequisites are missing.
var ErrUnavailable = errors.New("capture provider unavailable")

// CaptureProvider is implemented by every event source.
type CaptureProvider interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan WirelessEvent
	Status() ProviderStatus
}

// Commander is implemented by providers that can forward host→device
// command lines over their link. WRAITH Core's only use is the rate-limited
// deauth test transmission path (docs/tx.md).
type Commander interface {
	// SendCommand writes one complete command line (no trailing newline is
	// needed; implementations add it). The call must be safe for concurrent use.
	SendCommand(line string) error
}

// Manager owns the registered providers.
type Manager struct {
	mu        sync.RWMutex
	providers []CaptureProvider
}

// NewManager returns a manager with the given providers.
func NewManager(ps ...CaptureProvider) *Manager { return &Manager{providers: ps} }

// Statuses returns the status of every provider.
func (m *Manager) Statuses() []ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ProviderStatus, 0, len(m.providers))
	for _, p := range m.providers {
		out = append(out, p.Status())
	}
	return out
}

// StartAll starts every provider. Failures are reported per provider and never
// abort the others or the application.
func (m *Manager) StartAll(ctx context.Context) map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	errs := map[string]error{}
	for _, p := range m.providers {
		if err := p.Start(ctx); err != nil {
			errs[p.Name()] = err
		}
	}
	return errs
}

// StopAll stops every provider.
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.providers {
		_ = p.Stop()
	}
}

// SendCommand forwards a command line to the first provider implementing
// Commander (the ESP32 serial provider). It fails when no such provider
// exists or the write fails; link state is checked by the caller.
func (m *Manager) SendCommand(line string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var errs []error
	for _, p := range m.providers {
		c, ok := p.(Commander)
		if !ok {
			continue
		}
		if err := c.SendCommand(line); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		} else {
			return nil
		}
	}
	if errs == nil {
		return errors.New("no provider accepts commands")
	}
	return errors.Join(errs...)
}

// Stub is a provider whose capture path is not implemented yet but whose
// availability is probed honestly. Embedded by the concrete Phase-1 providers.
type Stub struct {
	name   string
	mu     sync.Mutex
	status ProviderStatus
	ch     chan WirelessEvent
	once   sync.Once
}

// NewStub creates a stub with an initial availability verdict.
func NewStub(name string, available bool, msg string) *Stub {
	st := StateStopped
	if !available {
		st = StateUnavailable
	}
	return &Stub{name: name, ch: make(chan WirelessEvent),
		status: ProviderStatus{Name: name, State: st, Message: msg, Since: time.Now()}}
}

func (s *Stub) Name() string                 { return s.name }
func (s *Stub) Events() <-chan WirelessEvent { return s.ch }
func (s *Stub) Stop() error                  { s.once.Do(func() { close(s.ch) }); return nil }
func (s *Stub) Status() ProviderStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start reports why the provider cannot run.
func (s *Stub) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State == StateUnavailable {
		return ErrUnavailable
	}
	return ErrNotImplemented
}
