package ingest_test

// End-to-end validation of the deauthentication-burst detection path through
// the real serial reader: pty -> transport framing + CRC -> events.Parse ->
// ingest pipeline -> detection -> finding in SQLite.
//
// This does NOT transmit deauth frames anywhere. It feeds synthetic
// wifi.deauth_observed events, byte-for-byte as WRAITH Nano serializes them,
// into a local pseudo-terminal. It validates every link except the ESP32's
// radio physically receiving the frames (covered separately by the
// single-frame parser hardware test and the firmware mgmt-monitor host test).

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/MoriartyPuth/wraith/core/internal/capture/serial"
	"github.com/MoriartyPuth/wraith/core/internal/detection"
	"github.com/MoriartyPuth/wraith/core/internal/ingest"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/transport"
)

func newPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0)
	n, _ := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	return os.NewFile(uintptr(fd), "ptmx"), fmt.Sprintf("/dev/pts/%d", n)
}

// deauthLine builds one framed, checksummed wifi.deauth_observed line as the
// firmware would, with count broadcast frames and reason code 7.
func deauthLine(boot string, seq, count int, bssid string) string {
	js := fmt.Sprintf(`{"version":1,"event_id":"%s-%d","source":"wraith-nano","sensor_id":"nano-70b2a8",`+
		`"event_type":"wifi.deauth_observed","timestamp":0,"seq":%d,"channel":6,"bssid":"%s",`+
		`"metadata":{"count":%d,"window_ms":1000,"src":"%s","dst":"FF:FF:FF:FF:FF:FF","reason_codes":[7],"protected":false}}`,
		boot, seq, seq, bssid, count, bssid)
	return string(transport.AppendChecksum([]byte(js))) + "\n"
}

func TestDeauthBurstEndToEndOverSerial(t *testing.T) {
	master, slave := newPTY(t)
	defer master.Close()

	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pipe := ingest.New(st, detection.NewEngine(), nil, log)

	p := serial.New(serial.Options{Port: slave, OnReject: pipe.Rejected})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	go pipe.Run(ctx, p.Events())

	// Wait for the reader to be running.
	for i := 0; i < 200 && p.Status().State != "running"; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	// A burst: 6 reports of 8 broadcast deauth frames for one BSSID = 48
	// frames, over the detector's 60 s window (threshold 30 → suspicious).
	const bssid = "AA:BB:CC:DD:EE:01"
	for i := 0; i < 6; i++ {
		if _, err := master.Write([]byte(deauthLine("e2e00001", i, 8, bssid))); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for the finding to be stored.
	var f storage.Finding
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fs, n, err := st.ListFindings(ctx, storage.Page{Limit: 10})
		if err == nil && n >= 1 {
			f = fs[0]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if f.ID == "" {
		t.Fatal("no finding produced from the deauth burst")
	}
	if f.Category != "deauth_activity" || f.Classification != "suspicious" {
		t.Fatalf("unexpected finding: %+v", f)
	}
	if f.BSSID == nil || *f.BSSID != bssid {
		t.Fatalf("finding BSSID = %v", f.BSSID)
	}
	if len(f.Evidence) < 3 {
		t.Fatalf("evidence only %d events", len(f.Evidence))
	}
	// The cited evidence must resolve to stored events.
	_, n, _ := st.ListEvents(ctx, storage.Page{Limit: 64}, storage.EventFilter{EventIDs: f.Evidence})
	if n != len(f.Evidence) {
		t.Fatalf("evidence lookup: %d of %d", n, len(f.Evidence))
	}
	if p.Status().Stats["valid"] < 6 || p.Status().Stats["checksum_failed"] != 0 {
		t.Fatalf("link stats: %v", p.Status().Stats)
	}
}
