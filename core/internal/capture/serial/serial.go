// Package serial reads WRAITH Nano events from a USB serial port.
//
// The port is opened raw at the configured baud rate. Lines are framed by
// transport.LineReader and validated by events.Parse. If the device is
// missing or disappears, the provider retries with exponential backoff (hot
// plug). Opening the port does not toggle DTR/RTS deliberately, so the ESP32
// is not reset by Core.
package serial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/transport"
)

const name = "esp32-serial"

// Startup grace: some USB-serial adapters (observed on this project's CP2102
// board) replay stale packets for several seconds after the port is opened
// following an idle period — measured at >800 KB/s, i.e. duplicated on the
// USB side. Nothing is discarded blindly: intact, checksum-valid lines in the
// burst are still ingested; junk is counted as replayed_on_open and does not
// mark the link degraded during this grace period.
const startupGrace = 30 * time.Second

// pingInterval: Core tells the sensor it is listening (shown on its LCD as
// "HOST: CONNECTED"). The sensor's timeout is 6 s.
const pingInterval = 2 * time.Second

// Link-health thresholds: within one window, a link that delivers more than
// junkMin junk lines (non-protocol or failed checksum) and junkRatio times as
// many junk lines as valid events is reported as degraded.
const (
	healthWindow = 10 * time.Second
	junkMin      = 50
	junkRatio    = 5
)

// Options configure the provider.
type Options struct {
	Port     string
	Baud     int
	OnReject func(capture.Rejection)      // may be nil
	OnStatus func(capture.ProviderStatus) // may be nil; called on state changes
	MinRetry time.Duration                // default 1 s
	MaxRetry time.Duration                // default 5 s (local device: retrying is cheap)
	// StallTimeout: if an open port delivers no bytes for this long, it is
	// closed and reopened. Nano sends a heartbeat every 5 s, so silence means
	// the read path is stuck (observed after adapter replay storms: reads on
	// the open file never resume, while a fresh open works). Default 15 s.
	StallTimeout time.Duration
}

// Provider implements capture.CaptureProvider for one serial port.
type Provider struct {
	opt    Options
	events chan capture.WirelessEvent

	mu     sync.Mutex
	status capture.ProviderStatus
	cancel context.CancelFunc
	done   chan struct{}
	file   *os.File

	lines, valid, nonProtocol, oversize, rejected, partial, reconnects, crcFailed, replayed, pings, stalls atomic.Uint64
	commands                                                                                               atomic.Uint64
}

// New returns the provider; an empty port yields an "unavailable" stub.
func New(opt Options) capture.CaptureProvider {
	if opt.Port == "" {
		return capture.NewStub(name, false, "no serial port configured (-serial)")
	}
	if opt.Baud == 0 {
		opt.Baud = 115200
	}
	if opt.MinRetry == 0 {
		opt.MinRetry = time.Second
	}
	if opt.MaxRetry == 0 {
		opt.MaxRetry = 5 * time.Second
	}
	if opt.StallTimeout == 0 {
		opt.StallTimeout = 15 * time.Second
	}
	p := &Provider{opt: opt, events: make(chan capture.WirelessEvent, 256)}
	p.status = capture.ProviderStatus{Name: name, State: capture.StateStopped, Message: opt.Port, Since: time.Now(), Implemented: true}
	return p
}

func (p *Provider) Name() string                         { return name }
func (p *Provider) Events() <-chan capture.WirelessEvent { return p.events }

// Status returns the current state and counters.
func (p *Provider) Status() capture.ProviderStatus {
	p.mu.Lock()
	st := p.status
	p.mu.Unlock()
	st.Stats = map[string]uint64{
		"lines": p.lines.Load(), "valid": p.valid.Load(), "rejected": p.rejected.Load(),
		"non_protocol": p.nonProtocol.Load(), "oversize": p.oversize.Load(),
		"partial_at_disconnect": p.partial.Load(), "reconnects": p.reconnects.Load(),
		"checksum_failed": p.crcFailed.Load(), "replayed_on_open": p.replayed.Load(),
		"pings_sent": p.pings.Load(), "stall_reopens": p.stalls.Load(), "commands_sent": p.commands.Load(),
	}
	return st
}

// SendCommand writes one host→device command line (capture.Commander).
// Single-line ASCII, no control characters; the trailing newline is added
// here. Safe for concurrent use (the ping writer and hot-plug reconnects
// close/replace the file, so the handle is taken under the status lock).
func (p *Provider) SendCommand(line string) error {
	if len(line) == 0 || len(line) > 96 || strings.ContainsAny(line, "\n\r\t\x00") {
		return errors.New("invalid command line")
	}
	p.mu.Lock()
	f := p.file
	p.mu.Unlock()
	if f == nil {
		return fmt.Errorf("%s is not connected", p.opt.Port)
	}
	n, err := f.Write([]byte(line + "\n"))
	if err != nil {
		return fmt.Errorf("write %s: %w", p.opt.Port, err)
	}
	if n != len(line)+1 {
		return fmt.Errorf("write %s: short write (%d/%d bytes)", p.opt.Port, n, len(line)+1)
	}
	p.commands.Add(1)
	return nil
}

func (p *Provider) setState(s capture.State, msg string) {
	p.mu.Lock()
	changed := p.status.State != s || p.status.Message != msg
	if changed {
		p.status.State, p.status.Message, p.status.Since = s, msg, time.Now()
	}
	p.mu.Unlock()
	if changed && p.opt.OnStatus != nil {
		p.opt.OnStatus(p.Status())
	}
}

// Start begins reading in the background. It never fails for a missing
// device; the provider keeps retrying until Stop or ctx cancellation.
func (p *Provider) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return errors.New("already started")
	}
	ctx, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	p.mu.Unlock()
	go p.run(ctx)
	return nil
}

// Stop ends reading and closes the events channel.
func (p *Provider) Stop() error {
	p.mu.Lock()
	cancel, done, f := p.cancel, p.done, p.file
	p.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if f != nil {
		f.Close() // unblocks Read
	}
	<-done
	return nil
}

func (p *Provider) run(ctx context.Context) {
	defer close(p.done)
	defer close(p.events)
	delay := p.opt.MinRetry
	first := true
	for ctx.Err() == nil {
		p.setState(capture.StateStarting, "opening "+p.opt.Port)
		f, err := openRaw(p.opt.Port, p.opt.Baud)
		if err != nil {
			p.setState(capture.StateError, fmt.Sprintf("%s: %v (retrying in %s)", p.opt.Port, err, delay))
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, p.opt.MaxRetry)
			continue
		}
		if !first {
			p.reconnects.Add(1)
		}
		first = false
		delay = p.opt.MinRetry
		p.mu.Lock()
		p.file = f
		p.mu.Unlock()
		p.setState(capture.StateRunning, "reading "+p.opt.Port)

		connCtx, stopPing := context.WithCancel(ctx)
		go p.ping(connCtx, f)
		err = p.readLoop(ctx, f)
		stopPing()

		p.mu.Lock()
		p.file = nil
		p.mu.Unlock()
		f.Close()
		if ctx.Err() != nil {
			break
		}
		p.setState(capture.StateError, fmt.Sprintf("%s disconnected: %v", p.opt.Port, err))
	}
	p.setState(capture.StateStopped, "stopped")
}

// ping writes "PING\n" every pingInterval until ctx ends. Write errors are
// ignored here: a dead port is detected by the read loop.
func (p *Provider) ping(ctx context.Context, f *os.File) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		if _, err := f.Write([]byte("PING\n")); err == nil {
			p.pings.Add(1)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// health tracks junk vs valid lines per window and flips the provider
// between running and degraded.
type health struct {
	start, opened time.Time
	junk, valid   int
}

func (p *Provider) checkHealth(h *health, now time.Time) {
	if now.Sub(h.start) < healthWindow {
		return
	}
	if now.Sub(h.opened) < startupGrace {
		*h = health{start: now, opened: h.opened}
		return
	}
	bad := h.junk >= junkMin && h.junk > junkRatio*h.valid
	switch {
	case bad:
		p.setState(capture.StateDegraded, fmt.Sprintf(
			"%s is corrupting data: %d unusable lines vs %d valid events in the last %s. "+
				"This points to the USB-serial adapter or cable (duplicated USB packets); corrupted lines are discarded, "+
				"not stored. Try another USB port (avoid hubs) or a shorter cable.",
			p.opt.Port, h.junk, h.valid, healthWindow))
	case p.Status().State == capture.StateDegraded:
		p.setState(capture.StateRunning, "reading "+p.opt.Port+" (link recovered)")
	}
	*h = health{start: now, opened: h.opened}
}

// junkLine counts one unusable line, separating the post-open replay burst.
func (p *Provider) junkLine(h *health, now time.Time, counter *atomic.Uint64) {
	if now.Sub(h.opened) < startupGrace {
		p.replayed.Add(1)
	} else {
		counter.Add(1)
	}
	h.junk++
}

func (p *Provider) readLoop(ctx context.Context, f *os.File) error {
	lr := transport.NewLineReader(f, events.MaxMessageSize)
	first := true
	opened := time.Now()
	h := health{start: opened, opened: opened}
	for {
		p.checkHealth(&h, time.Now())
		// Each read must make progress within StallTimeout (see Options).
		_ = f.SetReadDeadline(time.Now().Add(p.opt.StallTimeout))
		line, err := lr.Next()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				p.stalls.Add(1)
				return fmt.Errorf("no data for %s; reopening (stalled read)", p.opt.StallTimeout)
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				p.partial.Add(1)
			}
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF // a serial port does not legitimately end
			}
			return err
		}
		p.lines.Add(1)
		switch line.Kind {
		case transport.LineLog:
			p.junkLine(&h, time.Now(), &p.nonProtocol)
			first = false
			continue
		case transport.LineOversize:
			p.junkLine(&h, time.Now(), &p.oversize)
			p.reject("", events.ReasonOversize, fmt.Sprintf("%d bytes", line.Size))
			first = false
			continue
		}
		// Integrity first: a line whose CRC does not match was corrupted in
		// transit (e.g. USB-serial adapters replaying packets) and must not
		// be stored, even if it happens to be valid JSON.
		payload, crc := transport.SplitChecksum(line.Data)
		if crc != transport.ChecksumOK {
			first = false
			p.crcFailed.Add(1) // corrupted event lines stay visible, even in the grace period
			h.junk++
			reason := "checksum_mismatch"
			if crc == transport.ChecksumAbsent {
				reason = "missing_checksum"
			}
			p.reject("", reason, fmt.Sprintf("%d bytes", len(line.Data)))
			continue
		}
		ev, err := events.Parse(payload)
		if err != nil {
			var ve *events.ValidationError
			reason := "invalid"
			if errors.As(err, &ve) {
				reason = ve.Reason
			}
			// The first line after opening is often the tail of a line that
			// was mid-transmission; count it as non-protocol, not a rejection.
			if first && reason == events.ReasonMalformedJSON {
				p.nonProtocol.Add(1)
				first = false
				continue
			}
			p.reject(sensorIDOf(payload), reason, err.Error())
			first = false
			continue
		}
		first = false
		h.valid++
		p.valid.Add(1)
		ev.ReceivedAt = time.Now()
		select {
		case p.events <- *ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *Provider) reject(sensor, reason, detail string) {
	p.rejected.Add(1)
	if p.opt.OnReject != nil {
		p.opt.OnReject(capture.Rejection{Provider: name, SensorID: sensor, Reason: reason, Detail: detail})
	}
}

// sensorIDOf extracts sensor_id from a line that failed validation, if it is
// at least JSON with a plausible id.
func sensorIDOf(raw []byte) string {
	var v struct {
		SensorID string `json:"sensor_id"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.SensorID) == 0 || len(v.SensorID) > 64 {
		return ""
	}
	for _, c := range v.SensorID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '-') {
			return ""
		}
	}
	return v.SensorID
}

var baudRates = map[int]uint32{9600: unix.B9600, 19200: unix.B19200, 38400: unix.B38400, 57600: unix.B57600,
	115200: unix.B115200, 230400: unix.B230400, 460800: unix.B460800, 921600: unix.B921600}

// openRaw opens a tty non-blocking (so Close unblocks Read via the runtime
// poller), puts it in raw 8N1 mode and discards stale buffered input.
func openRaw(path string, baud int) (*os.File, error) {
	speed, ok := baudRates[baud]
	if !ok {
		return nil, fmt.Errorf("unsupported baud rate %d", baud)
	}
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var ioErr error
	// Control does not switch the fd to blocking mode (f.Fd() would).
	err = rc.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if err != nil {
			ioErr = fmt.Errorf("not a terminal: %w", err)
			return
		}
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
		t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | speed
		t.Ispeed, t.Ospeed = speed, speed
		t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
		if err := unix.IoctlSetTermios(int(fd), unix.TCSETS, t); err != nil {
			ioErr = fmt.Errorf("set termios: %w", err)
			return
		}
		_ = unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH) // drop stale input
	})
	if err == nil {
		err = ioErr
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
