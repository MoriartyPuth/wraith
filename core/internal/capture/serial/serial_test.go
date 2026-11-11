package serial

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/transport"
)

// newPTY returns the master side and the slave device path. The slave behaves
// like a serial port (termios works on it).
func newPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pty support: %v", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(fd), "ptmx"), fmt.Sprintf("/dev/pts/%d", n)
}

const hbJSON = `{"version":1,"event_id":"aaaa0001-%d","source":"wraith-nano","sensor_id":"nano-1","event_type":"sensor.heartbeat","timestamp":0,"seq":%d,"metadata":{"uptime_ms":1,"free_heap":1,"queue_drops":0}}`

// hbLine returns a framed heartbeat line (JSON + checksum, no newline).
func hbLine(boot, seq int) string {
	return string(transport.AppendChecksum([]byte(fmt.Sprintf(hbJSON, boot, seq))))
}

func waitEvent(t *testing.T, ch <-chan capture.WirelessEvent) capture.WirelessEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
	return capture.WirelessEvent{}
}

func waitState(t *testing.T, p capture.CaptureProvider, want capture.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p.Status().State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state %s, want %s (%s)", p.Status().State, want, p.Status().Message)
}

func TestReadsFragmentedLinesAndRejects(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	var mu sync.Mutex
	var rejects []capture.Rejection
	p := New(Options{Port: slave, OnReject: func(r capture.Rejection) { mu.Lock(); rejects = append(rejects, r); mu.Unlock() }})
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	waitState(t, p, capture.StateRunning)

	// Opening flushed stale input; a partial line first (tail of an older
	// line) must be ignored, then a log line, an event split across writes,
	// an invalid event with a recognisable sensor id, and a valid event.
	writes := []string{
		"eue_drops\":0}}\n",
		"I (27) boot: ESP-IDF 6.1.0\r\n",
		hbLine(0, 0)[:40], hbLine(0, 0)[40:] + "\r\n",
		string(transport.AppendChecksum([]byte(`{"version":1,"event_id":"x-1","source":"wraith-nano","sensor_id":"nano-1","event_type":"sensor.heartbeat","timestamp":0,"seq":1,"metadata":{"bogus":1}}`))) + "\n",
		// replayed USB packet spliced into a line: valid JSON shape, wrong CRC
		strings.Replace(hbLine(3, 3), `"uptime_ms":1`, `"uptime_ms":1,"uptime_ms":1`, 1) + "\n",
		fmt.Sprintf(hbJSON, 4, 4) + "\n", // unframed: rejected
		hbLine(2, 2) + "\n",
	}
	for _, w := range writes {
		if _, err := master.Write([]byte(w)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ev := waitEvent(t, p.Events()); ev.EventID != "aaaa0001-0" || len(ev.Raw) == 0 || ev.ReceivedAt.IsZero() {
		t.Fatalf("event 1: %+v", ev)
	}
	if ev := waitEvent(t, p.Events()); ev.EventID != "aaaa0001-2" {
		t.Fatalf("event 2: %+v", ev)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(rejects) != 3 || rejects[0].SensorID != "nano-1" || rejects[0].Reason != "unknown_field" ||
		rejects[1].Reason != "checksum_mismatch" || rejects[1].SensorID != "" || rejects[2].Reason != "missing_checksum" {
		t.Fatalf("rejects: %+v", rejects)
	}
	st := p.Status().Stats
	// Within the startup grace, non-protocol lines count as replayed_on_open;
	// checksum failures always count as checksum_failed.
	if st["valid"] != 2 || st["rejected"] != 3 || st["replayed_on_open"] != 2 || st["checksum_failed"] != 2 || st["non_protocol"] != 0 {
		t.Fatalf("stats: %v", st)
	}
}

func TestReconnectsAfterUnplug(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "ttyNANO") // stable name, like /dev/serial/by-id/...
	master, slave := newPTY(t)
	if err := os.Symlink(slave, link); err != nil {
		t.Fatal(err)
	}
	p := New(Options{Port: link, MinRetry: 20 * time.Millisecond, MaxRetry: 100 * time.Millisecond})
	p.Start(context.Background())
	defer p.Stop()
	waitState(t, p, capture.StateRunning)
	master.Write([]byte(hbLine(0, 0) + "\n"))
	waitEvent(t, p.Events())

	// "Unplug": closing the master makes reads on the slave fail.
	master.Close()
	os.Remove(link)
	waitState(t, p, capture.StateError)

	// "Replug" as a new device behind the same name.
	master2, slave2 := newPTY(t)
	defer master2.Close()
	os.Symlink(slave2, link)
	waitState(t, p, capture.StateRunning)
	time.Sleep(50 * time.Millisecond)
	master2.Write([]byte(hbLine(1, 1) + "\n"))
	if ev := waitEvent(t, p.Events()); ev.EventID != "aaaa0001-1" {
		t.Fatalf("after reconnect: %+v", ev)
	}
	if p.Status().Stats["reconnects"] != 1 {
		t.Fatalf("reconnects = %d", p.Status().Stats["reconnects"])
	}
}

func TestMissingDeviceRetriesWithoutCrash(t *testing.T) {
	p := New(Options{Port: "/dev/does-not-exist-wraith", MinRetry: 10 * time.Millisecond, MaxRetry: 20 * time.Millisecond})
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail for a missing device: %v", err)
	}
	waitState(t, p, capture.StateError)
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung while retrying")
	}
	if _, ok := <-p.Events(); ok {
		t.Fatal("events channel not closed after Stop")
	}
}

func TestNotATerminal(t *testing.T) {
	f := filepath.Join(t.TempDir(), "plain")
	os.WriteFile(f, nil, 0o600)
	if _, err := openRaw(f, 115200); err == nil {
		t.Fatal("regular file accepted as serial port")
	}
	if _, err := openRaw("/dev/null", 12345); err == nil {
		t.Fatal("unsupported baud accepted")
	}
}

func TestEmptyPortIsUnavailable(t *testing.T) {
	if st := New(Options{}).Status(); st.State != capture.StateUnavailable {
		t.Fatalf("state %s", st.State)
	}
}

func TestLinkHealthDegradedAndRecovers(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave}).(*Provider)
	old := time.Now().Add(-time.Hour) // opened long ago: past the startup grace
	h := &health{start: time.Now().Add(-healthWindow), opened: old, junk: 500, valid: 3}
	// Junk-dominated window: degraded.
	p.setState(capture.StateRunning, "x")
	p.checkHealth(h, time.Now())
	if st := p.Status(); st.State != capture.StateDegraded || !strings.Contains(st.Message, "USB-serial adapter") {
		t.Fatalf("not degraded: %+v", st)
	}
	// A clean window recovers.
	*h = health{start: time.Now().Add(-healthWindow), opened: old, junk: 2, valid: 10}
	p.checkHealth(h, time.Now())
	if st := p.Status(); st.State != capture.StateRunning || !strings.Contains(st.Message, "recovered") {
		t.Fatalf("not recovered: %+v", st)
	}
	// Few junk lines (e.g. boot log after a reset) never degrade.
	*h = health{start: time.Now().Add(-healthWindow), opened: old, junk: 40, valid: 0}
	p.checkHealth(h, time.Now())
	if p.Status().State != capture.StateRunning {
		t.Fatal("boot log degraded the link")
	}
}

func TestJunkFloodEndToEnd(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave}).(*Provider)
	p.Start(context.Background())
	defer p.Stop()
	waitState(t, p, capture.StateRunning)
	// Replayed packet tails, as observed on the real CP2102 link.
	junk := strings.Repeat(`pr":false},"count":15}}`+"\r\n", 200)
	master.Write([]byte(hbLine(0, 0) + "\n" + junk + hbLine(1, 1) + "\n"))
	waitEvent(t, p.Events())
	waitEvent(t, p.Events())
	if st := p.Status().Stats; st["replayed_on_open"] < 200 || st["valid"] != 2 {
		t.Fatalf("stats: %v", st)
	}
}

func TestOpenBurstIsGraceNotDegraded(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave}).(*Provider)
	p.Start(context.Background())
	defer p.Stop()
	waitState(t, p, capture.StateRunning)
	// Replay burst with an intact line inside it: the line is ingested, the
	// junk is counted as replayed_on_open, not as non_protocol.
	junk := strings.Repeat(`pr":false},"count":15}}`+"\r\n", 300)
	master.Write([]byte(junk + hbLine(0, 0) + "\n" + junk))
	if ev := waitEvent(t, p.Events()); ev.EventID != "aaaa0001-0" {
		t.Fatalf("intact line lost in burst: %+v", ev)
	}
	time.Sleep(100 * time.Millisecond)
	st := p.Status()
	if st.Stats["replayed_on_open"] < 600 || st.Stats["non_protocol"] != 0 || st.State != capture.StateRunning {
		t.Fatalf("burst accounting: %s %v", st.State, st.Stats)
	}
	// Health ignores the grace period even for a junk-only window.
	h := &health{start: time.Now().Add(-healthWindow), opened: time.Now(), junk: 5000}
	p.checkHealth(h, time.Now())
	if p.Status().State != capture.StateRunning {
		t.Fatal("degraded during startup grace")
	}
}

func TestPingsHost(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave})
	p.Start(context.Background())
	defer p.Stop()
	waitState(t, p, capture.StateRunning)
	master.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := master.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "PING\n") {
		t.Fatalf("no ping: %q %v", buf[:n], err)
	}
}

func TestStalledReadReopens(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave, StallTimeout: 300 * time.Millisecond, MinRetry: 10 * time.Millisecond}).(*Provider)
	p.Start(context.Background())
	defer p.Stop()
	waitState(t, p, capture.StateRunning)
	// Say nothing: the provider must reopen, then still read normally.
	deadline := time.Now().Add(3 * time.Second)
	for p.Status().Stats["stall_reopens"] == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p.Status().Stats["stall_reopens"] == 0 || p.Status().Stats["reconnects"] == 0 {
		t.Fatalf("no stall reopen: %v", p.Status().Stats)
	}
	waitState(t, p, capture.StateRunning)
	master.Write([]byte(hbLine(0, 0) + "\n"))
	if ev := waitEvent(t, p.Events()); ev.EventID != "aaaa0001-0" {
		t.Fatalf("after reopen: %+v", ev)
	}
}

func TestSendCommand(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()
	p := New(Options{Port: slave}).(*Provider)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	waitState(t, p, capture.StateRunning)

	// Drain the ping first.
	master.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 256)
	if n, err := master.Read(buf); err == nil && strings.Contains(string(buf[:n]), "PING\n") {
		// ignore; the TX line arrives in a later read
	}
	line := "TX_DEAUTH AA:BB:CC:DD:EE:01 11:22:33:44:55:66 1 100 7 6"
	if err := p.SendCommand(line); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		master.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := master.Read(buf)
		if err != nil {
			t.Fatalf("timeout reading command back: %v", err)
		}
		if strings.Contains(string(buf[:n]), line+"\n") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("command not echoed: %q", string(buf[:n]))
		}
	}
	if p.Status().Stats["commands_sent"] != 1 {
		t.Fatalf("commands_sent: %v", p.Status().Stats)
	}

	// Malformed lines are rejected without touching the port.
	for _, bad := range []string{"", "no\nnewline", "tab\there", strings.Repeat("x", 97)} {
		if err := p.SendCommand(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestSendCommandNotConnected(t *testing.T) {
	p := New(Options{Port: "/dev/wraith-nonexistent", MinRetry: time.Second}).(*Provider)
	p.Start(context.Background())
	defer p.Stop()
	if err := p.SendCommand("PING_CHECK"); err == nil {
		t.Fatal("send to disconnected port must fail")
	}
}
