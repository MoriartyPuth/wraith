// Package api serves the WRAITH REST API under /api/v1.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/system"
)

// Deps are the services the API needs.
type Deps struct {
	Store        *storage.Store
	Capture      *capture.Manager
	Log          *slog.Logger
	Started      time.Time
	AllowedHosts []string // extra hostnames accepted besides loopback names
	// Interfaces lists wireless interfaces; overridable in tests.
	Interfaces func() ([]system.WirelessInterface, error)
	// Stream serves /api/v1/ws (nil: 503).
	Stream http.Handler
	// RuntimeStats adds live counters (ingestion, stream) to /statistics.
	RuntimeStats func() map[string]uint64
	// Now is the clock (tests).
	Now func() time.Time
}

// SensorHeartbeatTimeout: a sensor with no heartbeat for this long is shown
// as disconnected. Nano sends one every 5 s.
const SensorHeartbeatTimeout = 15 * time.Second

const (
	defaultLimit = 50
	maxLimit     = 500
)

var (
	reMAC   = regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`)
	reIdent = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
)

// NewRouter builds the HTTP handler.
func NewRouter(d Deps) http.Handler {
	if d.Interfaces == nil {
		d.Interfaces = system.ListWirelessInterfaces
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	h := &handlers{d}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(hostGuard(d.AllowedHosts))
	r.Use(requestLogger(d.Log))
	r.Use(middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		// Long-lived: outside the request timeout.
		r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
			if d.Stream == nil {
				writeErr(w, http.StatusServiceUnavailable, "unavailable", "live stream not running")
				return
			}
			d.Stream.ServeHTTP(w, r)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(15 * time.Second))
			h.routes(r)
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusNotFound, "not_found", "no such endpoint")
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" not allowed")
		})
	})
	return r
}

func (h *handlers) routes(r chi.Router) {
	{
		r.Get("/health", h.health)
		r.Get("/system/info", h.systemInfo)
		r.Get("/sensors", h.listSensors)
		r.Get("/sensors/{id}", h.getSensor)
		r.Get("/access-points", h.listAPs)
		r.Get("/access-points/{bssid}", h.getAP)
		r.Get("/access-points/{bssid}/observations", h.apObservations)
		r.Get("/events", h.listEvents)
		r.Get("/events/{id}", h.getEvent)
		r.Get("/detections", h.listFindings)
		r.Get("/detections/{id}", h.getFinding)
		r.Patch("/detections/{id}", h.patchFinding)
		r.Get("/capture/status", h.captureStatus)
		r.Get("/capture/interfaces", h.captureInterfaces)
		r.Get("/statistics", h.statistics)
	}
}

type handlers struct{ d Deps }

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	status, code := "ok", http.StatusOK
	db := "ok"
	if err := h.d.Store.Ping(r.Context()); err != nil {
		status, code, db = "degraded", http.StatusServiceUnavailable, err.Error()
	}
	writeJSON(w, code, map[string]any{
		"status": status,
		"time":   time.Now().UnixMilli(),
		"checks": map[string]string{"database": db},
	})
}

func (h *handlers) systemInfo(w http.ResponseWriter, r *http.Request) {
	info := system.Collect(h.d.Started)
	info.ProtocolVer = events.ProtocolVersion
	if v, err := h.d.Store.SchemaVersion(r.Context()); err == nil {
		info.SchemaVersion = v
	}
	writeJSON(w, http.StatusOK, info)
}

// sensorView adds liveness computed at request time.
type sensorView struct {
	storage.Sensor
	Connected bool `json:"connected"`
}

func (h *handlers) view(s storage.Sensor) sensorView {
	ok := s.LastHeartbeat != nil && h.d.Now().Sub(time.UnixMilli(*s.LastHeartbeat)) < SensorHeartbeatTimeout
	return sensorView{Sensor: s, Connected: ok}
}

func (h *handlers) listSensors(w http.ResponseWriter, r *http.Request) {
	paged(w, r, func(ctx context.Context, p storage.Page) ([]sensorView, int, error) {
		ss, n, err := h.d.Store.ListSensors(ctx, p)
		out := make([]sensorView, len(ss))
		for i, s := range ss {
			out[i] = h.view(s)
		}
		return out, n, err
	})
}

func (h *handlers) getSensor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !reIdent.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "invalid sensor id")
		return
	}
	one(w, r, func(ctx context.Context) (sensorView, error) {
		s, err := h.d.Store.GetSensor(ctx, id)
		return h.view(s), err
	})
}

func (h *handlers) listAPs(w http.ResponseWriter, r *http.Request) {
	paged(w, r, h.d.Store.ListAccessPoints)
}

func (h *handlers) getAP(w http.ResponseWriter, r *http.Request) {
	bssid := strings.ToUpper(chi.URLParam(r, "bssid"))
	if !reMAC.MatchString(bssid) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "bssid must be AA:BB:CC:DD:EE:FF")
		return
	}
	one(w, r, func(ctx context.Context) (storage.AccessPoint, error) { return h.d.Store.GetAccessPoint(ctx, bssid) })
}

func (h *handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	f, err := parseEventFilter(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}
	paged(w, r, func(ctx context.Context, p storage.Page) ([]storage.StoredEvent, int, error) {
		return h.d.Store.ListEvents(ctx, p, f)
	})
}

func (h *handlers) getEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "event id must be a positive integer")
		return
	}
	one(w, r, func(ctx context.Context) (storage.StoredEvent, error) { return h.d.Store.GetEvent(ctx, id) })
}

func (h *handlers) listFindings(w http.ResponseWriter, r *http.Request) {
	paged(w, r, h.d.Store.ListFindings)
}

func (h *handlers) getFinding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !reIdent.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "invalid finding id")
		return
	}
	one(w, r, func(ctx context.Context) (storage.Finding, error) { return h.d.Store.GetFinding(ctx, id) })
}

func (h *handlers) apObservations(w http.ResponseWriter, r *http.Request) {
	bssid := strings.ToUpper(chi.URLParam(r, "bssid"))
	if !reMAC.MatchString(bssid) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "bssid must be AA:BB:CC:DD:EE:FF")
		return
	}
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 5000 {
			writeErr(w, http.StatusBadRequest, "invalid_parameter", "limit must be 1-5000")
			return
		}
		limit = n
	}
	obs, err := h.d.Store.ListObservations(r.Context(), bssid, limit)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": obs})
}

func (h *handlers) patchFinding(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !reIdent.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "invalid finding id")
		return
	}
	// Requiring a JSON content type forces a CORS preflight for cross-site
	// requests, which Core never approves (no CORS headers): CSRF protection.
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "expected {\"status\": \"open|reviewed|dismissed\"}")
		return
	}
	switch body.Status {
	case "open", "reviewed", "dismissed":
	default:
		writeErr(w, http.StatusBadRequest, "invalid_body", "status must be open, reviewed or dismissed")
		return
	}
	f, err := h.d.Store.SetFindingStatus(r.Context(), id, body.Status, h.d.Now().UnixMilli())
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": f})
}

func parseEventFilter(r *http.Request) (storage.EventFilter, error) {
	q := r.URL.Query()
	var f storage.EventFilter
	if v := q.Get("type"); v != "" {
		for _, t := range strings.Split(v, ",") {
			if t == "" || len(t) > 64 {
				return f, errors.New("invalid type")
			}
			f.Types = append(f.Types, t)
		}
		if len(f.Types) > 16 {
			return f, errors.New("at most 16 types")
		}
	}
	if v := q.Get("event_id"); v != "" {
		for _, id := range strings.Split(v, ",") {
			if !reIdent.MatchString(id) {
				return f, errors.New("invalid event_id")
			}
			f.EventIDs = append(f.EventIDs, id)
		}
		if len(f.EventIDs) > 64 { // a finding cites at most 64 events
			return f, errors.New("at most 64 event ids")
		}
	}
	if v := q.Get("bssid"); v != "" {
		f.BSSID = strings.ToUpper(v)
		if !reMAC.MatchString(f.BSSID) {
			return f, errors.New("bssid must be AA:BB:CC:DD:EE:FF")
		}
	}
	if v := q.Get("sensor"); v != "" {
		if !reIdent.MatchString(v) {
			return f, errors.New("invalid sensor id")
		}
		f.SensorID = v
	}
	for key, dst := range map[string]*int64{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return f, fmt.Errorf("%s must be Unix milliseconds", key)
			}
			*dst = n
		}
	}
	return f, nil
}

func (h *handlers) captureStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.d.Capture.Statuses()})
}

func (h *handlers) captureInterfaces(w http.ResponseWriter, r *http.Request) {
	ifs, err := h.d.Interfaces()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "could not enumerate interfaces")
		h.d.Log.Error("list interfaces", "err", err)
		return
	}
	if ifs == nil {
		ifs = []system.WirelessInterface{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": ifs})
}

func (h *handlers) statistics(w http.ResponseWriter, r *http.Request) {
	st, err := h.d.Store.Statistics(r.Context())
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	var rt map[string]uint64
	if h.d.RuntimeStats != nil {
		rt = h.d.RuntimeStats()
	}
	writeJSON(w, http.StatusOK, struct {
		storage.Stats
		Runtime map[string]uint64 `json:"runtime"`
	}{st, rt})
}

// --- helpers ---

type pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func parsePage(r *http.Request) (storage.Page, error) {
	p := storage.Page{Limit: defaultLimit}
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return p, errors.New("limit must be 1-500")
		}
		p.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return p, errors.New("offset must be >= 0")
		}
		p.Offset = n
	}
	return p, nil
}

func paged[T any](w http.ResponseWriter, r *http.Request, fn func(context.Context, storage.Page) ([]T, int, error)) {
	p, err := parsePage(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}
	items, total, err := fn(r.Context(), p)
	if err != nil {
		internal(w, slog.Default(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       items,
		"pagination": pagination{Limit: p.Limit, Offset: p.Offset, Total: total},
	})
}

func one[T any](w http.ResponseWriter, r *http.Request, fn func(context.Context) (T, error)) {
	v, err := fn(r.Context())
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		internal(w, slog.Default(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": v})
}

func internal(w http.ResponseWriter, log *slog.Logger, err error) {
	log.Error("request failed", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal", "internal error")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

// hostGuard rejects requests whose Host header is not a loopback name or an
// explicitly allowed host. This blocks DNS-rebinding attacks from web pages
// against a Core bound to 127.0.0.1.
func hostGuard(extra []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, h := range extra {
		allowed[strings.ToLower(h)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.Trim(strings.ToLower(host), "[]")
			if !allowed[host] {
				writeErr(w, http.StatusForbidden, "forbidden_host", "host not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"dur_ms", time.Since(start).Milliseconds(), "req_id", middleware.GetReqID(r.Context()))
		})
	}
}
