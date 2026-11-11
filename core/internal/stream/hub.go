// Package stream fans out live updates to WebSocket clients.
//
// Each client has a bounded send queue. A client that cannot keep up is
// disconnected (it reconnects and resynchronises via REST) rather than
// stalling ingestion or silently skipping messages.
package stream

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/MoriartyPuth/wraith/core/internal/events"
)

const (
	writeWait     = 10 * time.Second
	pongWait      = 60 * time.Second
	pingPeriod    = 50 * time.Second
	maxReadBytes  = 512 // clients only send control frames
	clientBacklog = 256
)

// Message kinds.
const (
	KindEvent    = "event"
	KindAP       = "access_point"
	KindFinding  = "finding"
	KindSensor   = "sensor"
	KindClient   = "client"
	KindProvider = "provider"
	KindHello    = "hello"
)

// Message is the wire envelope.
type Message struct {
	Type string `json:"type"`
	Time int64  `json:"time"`
	Data any    `json:"data"`
}

// Hub tracks clients and broadcasts messages to them.
type Hub struct {
	mu       sync.Mutex
	clients  map[*client]struct{}
	closed   bool
	log      *slog.Logger
	upgrader websocket.Upgrader
	dropped  uint64 // clients disconnected for being too slow
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	once sync.Once
	done chan struct{}
}

// NewHub creates a hub. allowedHosts are extra Origin hostnames accepted in
// addition to loopback names.
func NewHub(log *slog.Logger, allowedHosts []string) *Hub {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, h := range allowedHosts {
		allowed[strings.ToLower(h)] = true
	}
	h := &Hub{clients: map[*client]struct{}{}, log: log}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin:     func(r *http.Request) bool { return originAllowed(r.Header.Get("Origin"), allowed) },
	}
	return h
}

// originAllowed blocks cross-site WebSocket hijacking: browsers always send
// Origin, and WebSockets are not covered by CORS. Non-browser clients that
// send no Origin are allowed (the Host guard still applies).
func originAllowed(origin string, allowed map[string]bool) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if h, _, err := net.SplitHostPort(u.Host); err == nil {
		host = h
	}
	return allowed[strings.ToLower(strings.Trim(host, "[]"))]
}

// Publish broadcasts a message to all clients. Never blocks.
func (h *Hub) Publish(kind string, data any) {
	b, err := json.Marshal(Message{Type: kind, Time: time.Now().UnixMilli(), Data: data})
	if err != nil {
		h.log.Error("stream marshal", "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- b:
		default:
			h.dropped++
			h.log.Warn("websocket client too slow; disconnecting")
			h.removeLocked(c)
		}
	}
}

// Clients returns the number of connected clients.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) removeLocked(c *client) {
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.once.Do(func() { close(c.done) })
	}
}

// ServeHTTP upgrades the connection and serves one client.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an HTTP error (e.g. 403 on bad Origin)
	}
	c := &client{conn: conn, send: make(chan []byte, clientBacklog), done: make(chan struct{})}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		conn.Close()
		return
	}
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	hello, _ := json.Marshal(Message{Type: KindHello, Time: time.Now().UnixMilli(), Data: map[string]any{"protocol": events.ProtocolVersion}})
	c.send <- hello
	go h.writePump(c)
	h.readPump(c)
}

func (h *Hub) readPump(c *client) {
	defer func() {
		h.mu.Lock()
		h.removeLocked(c)
		h.mu.Unlock()
	}()
	c.conn.SetReadLimit(maxReadBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(pongWait)) })
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			_ = c.conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "closing"), time.Now().Add(time.Second))
			return
		}
	}
}

// Close disconnects all clients and rejects new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		h.removeLocked(c)
	}
}
