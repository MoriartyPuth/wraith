package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/presence"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// Watchlist endpoints (docs/bluetooth/watchlist.md): the operator selects
// addresses to track; presence and sessions are derived by internal/presence
// from stored sightings. Only PUT/DELETE mutate anything, and PUT is
// JSON-only like every other write (CSRF guard).

const watchDayMS = int64(24 * time.Hour / time.Millisecond)

// watchView is a watchlist row plus the device snapshot and 24 h presence.
type watchView struct {
	storage.WatchEntry
	Device      *storage.BTDevice `json:"device,omitempty"`
	LastSeen    *int64            `json:"last_seen"`
	Sessions24h int               `json:"sessions_24h"`
	DwellMS24h  int64             `json:"dwell_ms_24h"`
}

func (h *handlers) watchlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ws, err := h.d.Store.ListWatchlist(ctx)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	now := time.Now().UnixMilli()
	since := now - watchDayMS
	out := make([]watchView, 0, len(ws))
	for _, we := range ws {
		v := watchView{WatchEntry: we}
		if dev, err := h.d.Store.GetBTDevice(ctx, we.Transport, we.Address, we.AddressType); err == nil {
			last := dev.LastSeen
			v.Device, v.LastSeen = &dev, &last
		} else if !errors.Is(err, storage.ErrNotFound) {
			internal(w, h.d.Log, err)
			return
		}
		times, err := h.d.Store.ListBTSightingTimes(ctx, we.Transport, we.Address, we.AddressType, since, 0)
		if err != nil {
			internal(w, h.d.Log, err)
			return
		}
		ss := presence.Build(times, now)
		v.Sessions24h = presence.CountSince(ss, since)
		v.DwellMS24h = presence.Dwell(ss, since, now)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *handlers) putWatch(w http.ResponseWriter, r *http.Request) {
	t, at, a, ok := btDeviceKey(w, r)
	if !ok {
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", `expected {"label":"..."} (label may be empty)`)
		return
	}
	label := ""
	if body.Label != "" {
		var err error
		if label, err = cleanLabel(body.Label); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}
	dev, err := h.d.Store.GetBTDevice(r.Context(), t, a, at)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "that device has not been observed")
		return
	}
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	now := time.Now().UnixMilli()
	// Seed the initial state from the current device, so adding a device that
	// is already present does not raise a false "arrived" finding.
	we := storage.WatchEntry{Transport: t, Address: a, AddressType: at, Label: label,
		AddedAt: now, Present: presence.Present(dev.LastSeen, now)}
	if we.Present {
		we.StateSince = &now
	}
	if err := h.d.Store.PutWatch(r.Context(), we); err != nil {
		internal(w, h.d.Log, err)
		return
	}
	got, err := h.d.Store.GetWatch(r.Context(), t, a, at)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": got})
}

func (h *handlers) deleteWatch(w http.ResponseWriter, r *http.Request) {
	t, at, a, ok := btDeviceKey(w, r)
	if !ok {
		return
	}
	switch err := h.d.Store.DeleteWatch(r.Context(), t, a, at); {
	case errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not on the watchlist")
	case err != nil:
		internal(w, h.d.Log, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handlers) watchSessions(w http.ResponseWriter, r *http.Request) {
	h.btSessions(w, r, true)
}

func (h *handlers) deviceSessions(w http.ResponseWriter, r *http.Request) {
	h.btSessions(w, r, false)
}

// btSessions returns presence sessions for one address. requireWatch makes
// the watchlist route 404 for an address the operator is not tracking.
func (h *handlers) btSessions(w http.ResponseWriter, r *http.Request, requireWatch bool) {
	ctx := r.Context()
	t, at, a, ok := btDeviceKey(w, r)
	if !ok {
		return
	}
	if requireWatch {
		if _, err := h.d.Store.GetWatch(ctx, t, a, at); errors.Is(err, storage.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "not on the watchlist")
			return
		} else if err != nil {
			internal(w, h.d.Log, err)
			return
		}
	} else if !h.btExists(w, r, t, a, at) {
		return
	}
	now := time.Now().UnixMilli()
	since := now - watchDayMS
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid_parameter", "since must be Unix milliseconds")
			return
		}
		since = n
	}
	times, err := h.d.Store.ListBTSightingTimes(ctx, t, a, at, since, 0)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	ss := presence.Build(times, now)
	var last int64
	if len(times) > 0 {
		last = times[len(times)-1]
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"now": now, "since": since, "present": presence.Present(last, now),
		"sessions": ss, "dwell_ms": presence.Dwell(ss, since, now),
	}})
}
