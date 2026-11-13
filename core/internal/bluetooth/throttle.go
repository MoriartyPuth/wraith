package bluetooth

import (
	"sync"
	"time"
)

// Live-stream limits for bt_device updates. The WebSocket hub disconnects a
// client that falls 256 messages behind, and Wi-Fi and Bluetooth share that
// queue, so a crowded BLE environment must not be able to fill it. Every
// update is still stored; only its live push is skipped (the dashboard
// re-fetches over REST).
const (
	StreamPerDevice = 2 * time.Second // at most one update per device per period
	StreamRate      = 20.0            // sustained device updates per second, all devices
	StreamBurst     = 40.0            // short bursts above the sustained rate
	maxTracked      = 4096            // bound on remembered devices
)

// Throttle decides whether a device update is pushed to the live stream. It
// combines a per-device minimum interval with a global token bucket. Safe for
// concurrent use; the ingest pipeline calls it from one goroutine.
type Throttle struct {
	mu         sync.Mutex
	perKey     time.Duration
	rate       float64
	burst      float64
	tokens     float64
	refilled   time.Time
	last       map[string]time.Time
	suppressed uint64
}

// NewThrottle returns a throttle with the stream limits above.
func NewThrottle() *Throttle {
	return &Throttle{perKey: StreamPerDevice, rate: StreamRate, burst: StreamBurst,
		tokens: StreamBurst, last: make(map[string]time.Time)}
}

// Allow reports whether an update for key may be published at now.
func (t *Throttle) Allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.refilled.IsZero() {
		if dt := now.Sub(t.refilled).Seconds(); dt > 0 {
			t.tokens += dt * t.rate
			if t.tokens > t.burst {
				t.tokens = t.burst
			}
		}
	}
	t.refilled = now
	if last, ok := t.last[key]; ok && now.Sub(last) < t.perKey {
		t.suppressed++
		return false
	}
	if t.tokens < 1 {
		t.suppressed++
		return false
	}
	t.tokens--
	if len(t.last) >= maxTracked {
		t.forget(now)
	}
	t.last[key] = now
	return true
}

// forget drops devices whose interval has passed (they would be allowed
// anyway). If every entry is recent, it clears the map: at worst one extra
// update per device, still bounded by the token bucket.
func (t *Throttle) forget(now time.Time) {
	for k, at := range t.last {
		if now.Sub(at) >= t.perKey {
			delete(t.last, k)
		}
	}
	if len(t.last) >= maxTracked {
		clear(t.last)
	}
}

// Suppressed returns how many updates were not pushed.
func (t *Throttle) Suppressed() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.suppressed
}
