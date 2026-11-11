// Tests for POST /api/v1/tx: the deauth test transmission path (docs/tx.md).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/system"
)

// fakeCommander is a provider that records the last command line.
type fakeCommander struct {
	capture.Stub
	mu    sync.Mutex
	line  string
	err   error
	state capture.State
}

func newFakeCommander() *fakeCommander {
	return &fakeCommander{Stub: *capture.NewStub("esp32-serial", true, "test")}
}

func (f *fakeCommander) setRunning() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = capture.StateRunning
}

func (f *fakeCommander) Status() capture.ProviderStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return capture.ProviderStatus{Name: "esp32-serial", State: f.state, Message: "test", Since: time.Now()}
}

func (f *fakeCommander) SendCommand(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.line = line
	return nil
}

func (f *fakeCommander) sentLine() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.line
}

func newTxServer(t *testing.T, txEnabled bool, p *fakeCommander) (*httptest.Server, *storage.Store) {
	t.Helper()
	st, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if p == nil {
		p = newFakeCommander()
	}
	h := NewRouter(Deps{
		Store: st, Capture: capture.NewManager(p), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Started: time.Now(), TXEnabled: txEnabled,
		Interfaces: func() ([]system.WirelessInterface, error) { return nil, nil },
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, st
}

// upsertAP inserts an observed AP with the given channel.
func upsertAP(t *testing.T, st *storage.Store, bssid string, channel int) {
	t.Helper()
	ch := channel
	ctx := context.Background()
	err := st.WithTx(ctx, func(tx *storage.Tx) error {
		return tx.UpsertAP(ctx, storage.APUpdate{BSSID: bssid, Seen: 1, Channel: &ch, Sensor: "nano-1"})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func post(t *testing.T, srv *httptest.Server, ct, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/tx", strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func errCode(t *testing.T, body map[string]any) string {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error object: %v", body)
	}
	return e["code"].(string)
}

func TestTXDisabledByDefault(t *testing.T) {
	srv, _ := newTxServer(t, false, nil)
	code, body := post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusForbidden || errCode(t, body) != "tx_disabled" {
		t.Fatalf("disabled: %d %v", code, body)
	}
}

func TestTXGates(t *testing.T) {
	srv, _ := newTxServer(t, true, nil)
	cases := []struct {
		name string
		ct   string
		body string
		code int
		err  string
	}{
		{"confirm required", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66"}`, 400, "confirm_required"},
		{"form content type", "application/x-www-form-urlencoded", `{"kind":"deauth","confirm":true}`, 415, ""},
		{"unknown field", "application/json", `{"kind":"deauth","confirm":true,"bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","x":1}`, 400, ""},
		{"bad kind", "application/json", `{"kind":"evil","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`, 400, ""},
		{"bad bssid", "application/json", `{"kind":"deauth","bssid":"aa:bb","client":"11:22:33:44:55:66","confirm":true}`, 400, ""},
		{"bad client", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"zz","confirm":true}`, 400, ""},
		{"broadcast client", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"FF:FF:FF:FF:FF:FF","confirm":true}`, 400, ""},
		{"client equals bssid", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"AA:BB:CC:DD:EE:01","confirm":true}`, 400, ""},
		{"reason out of range", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","reason":70000,"confirm":true}`, 400, ""},
		{"count zero", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","count":0,"confirm":true}`, 400, ""},
		{"count over cap", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","count":11,"confirm":true}`, 400, ""},
		{"interval too fast", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","interval_ms":99,"confirm":true}`, 400, ""},
		{"interval too slow", "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","interval_ms":10001,"confirm":true}`, 400, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := post(t, srv, c.ct, c.body)
			if code != c.code {
				t.Fatalf("status %d, want %d (%v)", code, c.code, body)
			}
			if c.err != "" && errCode(t, body) != c.err {
				t.Fatalf("code %v, want %s", body, c.err)
			}
		})
	}
}

func TestTXTargetNotObserved(t *testing.T) {
	srv, _ := newTxServer(t, true, nil)
	code, body := post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusConflict || errCode(t, body) != "target_not_observed" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestTXTargetChannelUnknown(t *testing.T) {
	srv, st := newTxServer(t, true, nil)
	// Observed, but the last channel is not a 2.4 GHz channel.
	upsertAP(t, st, "AA:BB:CC:DD:EE:02", 36)
	code, body := post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:02","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusConflict || errCode(t, body) != "target_channel_unknown" {
		t.Fatalf("%d %v", code, body)
	}
	// Observed with no channel at all.
	upsertAP(t, st, "AA:BB:CC:DD:EE:03", 0)
	code, body = post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:03","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusConflict || errCode(t, body) != "target_channel_unknown" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestTXRequiresSensorConnected(t *testing.T) {
	srv, st := newTxServer(t, true, nil)
	upsertAP(t, st, "AA:BB:CC:DD:EE:01", 6)
	// The fake provider is not running (stub state), so the request is refused.
	code, body := post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusConflict || errCode(t, body) != "sensor_not_connected" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestTXAccepted(t *testing.T) {
	p := newFakeCommander()
	p.setRunning()
	srv, st := newTxServer(t, true, p)
	upsertAP(t, st, "AA:BB:CC:DD:EE:01", 6)
	code, body := post(t, srv, "application/json",
		`{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","count":3,"interval_ms":500,"reason":7,"confirm":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %v", code, body)
	}
	want := "TX_DEAUTH AA:BB:CC:DD:EE:01 11:22:33:44:55:66 3 500 7 6"
	if got := p.sentLine(); got != want {
		t.Fatalf("command %q, want %q", got, want)
	}
	// Defaults: reason and shape.
	code, body = post(t, srv, "application/json",
		`{"kind":"disassoc","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %v", code, body)
	}
	want = "TX_DISASSOC AA:BB:CC:DD:EE:01 11:22:33:44:55:66 1 100 7 6"
	if got := p.sentLine(); got != want {
		t.Fatalf("command %q, want %q", got, want)
	}
}

func TestTXSendFailure(t *testing.T) {
	p := newFakeCommander()
	p.setRunning()
	p.err = errors.New("port closed")
	srv, st := newTxServer(t, true, p)
	upsertAP(t, st, "AA:BB:CC:DD:EE:01", 6)
	code, body := post(t, srv, "application/json", `{"kind":"deauth","bssid":"AA:BB:CC:DD:EE:01","client":"11:22:33:44:55:66","confirm":true}`)
	if code != http.StatusBadGateway || errCode(t, body) != "tx_send_failed" {
		t.Fatalf("%d %v", code, body)
	}
}
