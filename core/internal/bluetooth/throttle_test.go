package bluetooth

import (
	"fmt"
	"testing"
	"time"
)

func TestThrottlePerDevice(t *testing.T) {
	th := NewThrottle()
	t0 := time.Unix(1000, 0)
	if !th.Allow("a", t0) {
		t.Fatal("first update must pass")
	}
	if th.Allow("a", t0.Add(StreamPerDevice-time.Millisecond)) {
		t.Fatal("second update within the interval must be suppressed")
	}
	if !th.Allow("b", t0.Add(time.Millisecond)) {
		t.Fatal("another device is independent")
	}
	if !th.Allow("a", t0.Add(StreamPerDevice)) {
		t.Fatal("update after the interval must pass")
	}
	if th.Suppressed() != 1 {
		t.Fatalf("suppressed = %d", th.Suppressed())
	}
}

func TestThrottleGlobalRate(t *testing.T) {
	th := NewThrottle()
	t0 := time.Unix(1000, 0)
	// A flood of distinct devices in the same instant: only the burst passes.
	passed := 0
	for i := 0; i < 500; i++ {
		if th.Allow(fmt.Sprintf("dev-%d", i), t0) {
			passed++
		}
	}
	if passed != int(StreamBurst) {
		t.Fatalf("burst: %d passed, want %d", passed, int(StreamBurst))
	}
	// Over the next 10 s at 100 new devices/s, the sustained rate holds.
	passed = 0
	for i := 0; i < 1000; i++ {
		if th.Allow(fmt.Sprintf("late-%d", i), t0.Add(time.Duration(i+1)*10*time.Millisecond)) {
			passed++
		}
	}
	if want := int(10 * StreamRate); passed < want-1 || passed > want+1 {
		t.Fatalf("sustained: %d passed in 10 s, want ~%d", passed, want)
	}
}

func TestThrottleBoundedMemory(t *testing.T) {
	th := NewThrottle()
	t0 := time.Unix(1000, 0)
	for i := 0; i < 3*maxTracked; i++ {
		th.Allow(fmt.Sprintf("d-%d", i), t0.Add(time.Duration(i)*time.Second)) // tokens refill: all pass
	}
	if len(th.last) > maxTracked {
		t.Fatalf("tracked %d devices, cap %d", len(th.last), maxTracked)
	}
}
