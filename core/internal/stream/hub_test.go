package stream

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setup(t *testing.T) (*Hub, string) {
	h := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	srv := httptest.NewServer(h)
	t.Cleanup(func() { h.Close(); srv.Close() })
	return h, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, url, origin string) (*websocket.Conn, *http.Response, error) {
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial(url, hdr)
}

func read(t *testing.T, c *websocket.Conn) Message {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	json.Unmarshal(b, &m)
	return m
}

func TestBroadcast(t *testing.T) {
	h, url := setup(t)
	a, _, err := dial(t, url, "http://127.0.0.1:5173")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := dial(t, url, "")
	if err != nil {
		t.Fatal(err)
	}
	if read(t, a).Type != KindHello || read(t, b).Type != KindHello {
		t.Fatal("no hello")
	}
	for h.Clients() != 2 {
		time.Sleep(5 * time.Millisecond)
	}
	h.Publish(KindEvent, map[string]string{"event_id": "x-1"})
	for _, c := range []*websocket.Conn{a, b} {
		m := read(t, c)
		if m.Type != KindEvent || m.Data.(map[string]any)["event_id"] != "x-1" {
			t.Fatalf("got %+v", m)
		}
	}
}

func TestCrossSiteOriginRejected(t *testing.T) {
	_, url := setup(t)
	for _, o := range []string{"https://evil.example", "http://127.0.0.1.evil.example", "null", "file://"} {
		_, resp, err := dial(t, url, o)
		if err == nil {
			t.Fatalf("origin %q accepted", o)
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: status %d", o, resp.StatusCode)
		}
	}
}

func TestSlowClientDisconnectedNotBlocking(t *testing.T) {
	h, url := setup(t)
	c, _, err := dial(t, url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for h.Clients() != 1 {
		time.Sleep(5 * time.Millisecond)
	}
	// Never read from c. Publishing far more than the backlog must not block.
	done := make(chan struct{})
	go func() {
		big := strings.Repeat("x", 64*1024)
		for i := 0; i < clientBacklog*8; i++ {
			h.Publish(KindEvent, big)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a slow client")
	}
	if h.Clients() != 0 {
		t.Fatalf("slow client still connected")
	}
}

func TestCloseDisconnectsClients(t *testing.T) {
	h, url := setup(t)
	c, _, _ := dial(t, url, "")
	read(t, c)
	h.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("client still open after Close")
	}
	if _, _, err := dial(t, url, ""); err == nil {
		// Upgrade may succeed briefly then close; ensure no client is registered.
		if h.Clients() != 0 {
			t.Fatal("client registered after Close")
		}
	}
}
