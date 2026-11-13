package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// Bluetooth REST endpoints (docs/bluetooth/api.md). Read-only: the Bluetooth
// inventory is filled by ingest from protocol v4 events. Same router, host
// guard, timeout, pagination and error format as every other endpoint.
func (h *handlers) btRoutes(r chi.Router) {
	r.Get("/bluetooth/status", h.btStatus)
	r.Get("/bluetooth/devices", h.btDevices(""))
	r.Get("/bluetooth/classic/devices", h.btDevices(storage.BTClassic))
	r.Get("/bluetooth/ble/devices", h.btDevices(storage.BTBLE))
	r.Get("/bluetooth/devices/{id}", h.btDevice)
	r.Get("/bluetooth/devices/{id}/observations", h.btObservations)
	r.Get("/bluetooth/devices/{id}/events", h.btDeviceEvents)
	r.Get("/bluetooth/ble/devices/{id}/services", h.btGATT(events.TypeBtGattService))
	r.Get("/bluetooth/ble/devices/{id}/characteristics", h.btGATT(events.TypeBtGattChar))
	r.Get("/bluetooth/events", h.btEvents)
}

// btRecentWindow is what /bluetooth/status counts as "recently seen".
const btRecentWindow = 10 * time.Minute

// btGATTScan bounds how many GATT events one services/characteristics
// request reads.
const btGATTScan = 1000

func (h *handlers) btStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sensors, err := h.d.Store.ListBTStatus(ctx)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	count := func(transport string, since int64) (int, error) {
		_, n, err := h.d.Store.ListBTDevices(ctx, storage.Page{Limit: 1},
			storage.BTDeviceFilter{Transport: transport, Since: since})
		return n, err
	}
	recent := time.Now().Add(-btRecentWindow).UnixMilli()
	var c [4]int
	for i, q := range []struct {
		t     string
		since int64
	}{{storage.BTClassic, 0}, {storage.BTBLE, 0}, {storage.BTClassic, recent}, {storage.BTBLE, recent}} {
		if c[i], err = count(q.t, q.since); err != nil {
			internal(w, h.d.Log, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"sensors": sensors,
		"devices": map[string]any{
			"classic": c[0], "ble": c[1], "total": c[0] + c[1],
			"recent_classic": c[2], "recent_ble": c[3],
			"recent_window_ms": btRecentWindow.Milliseconds(),
		},
	}})
}

func (h *handlers) btDevices(forced string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := storage.BTDeviceFilter{Transport: forced}
		if v := q.Get("transport"); v != "" {
			if forced != "" || (v != storage.BTClassic && v != storage.BTBLE) {
				writeErr(w, http.StatusBadRequest, "invalid_parameter", "transport must be classic or ble")
				return
			}
			f.Transport = v
		}
		if v := q.Get("since"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeErr(w, http.StatusBadRequest, "invalid_parameter", "since must be Unix milliseconds")
				return
			}
			f.Since = n
		}
		paged(w, r, func(ctx context.Context, p storage.Page) ([]storage.BTDevice, int, error) {
			return h.d.Store.ListBTDevices(ctx, p, f)
		})
	}
}

// btDeviceKey parses and validates the {id} path parameter
// ("classic:AA:..", "ble:public:AA:..", "ble:random:AA:..", colons may be
// %3A-encoded).
func btDeviceKey(w http.ResponseWriter, r *http.Request) (transport, addrType, address string, ok bool) {
	transport, addrType, address, ok = storage.ParseBTDeviceID(pathParam(r, "id"))
	address = strings.ToUpper(address)
	if !ok || !reMAC.MatchString(address) {
		writeErr(w, http.StatusBadRequest, "invalid_parameter",
			"id must be classic:AA:BB:CC:DD:EE:FF or ble:<public|random>:AA:BB:CC:DD:EE:FF")
		return "", "", "", false
	}
	return transport, addrType, address, true
}

func (h *handlers) btDevice(w http.ResponseWriter, r *http.Request) {
	t, at, a, ok := btDeviceKey(w, r)
	if !ok {
		return
	}
	one(w, r, func(ctx context.Context) (storage.BTDevice, error) {
		return h.d.Store.GetBTDevice(ctx, t, a, at)
	})
}

func (h *handlers) btObservations(w http.ResponseWriter, r *http.Request) {
	t, at, a, ok := btDeviceKey(w, r)
	if !ok {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeErr(w, http.StatusBadRequest, "invalid_parameter", "limit must be 1-1000")
			return
		}
		limit = n
	}
	if !h.btExists(w, r, t, a, at) {
		return
	}
	obs, err := h.d.Store.ListBTObservations(r.Context(), t, a, at, limit)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": obs})
}

func (h *handlers) btDeviceEvents(w http.ResponseWriter, r *http.Request) {
	t, at, a, ok := btDeviceKey(w, r)
	if !ok || !h.btExists(w, r, t, a, at) {
		return
	}
	paged(w, r, func(ctx context.Context, p storage.Page) ([]storage.StoredEvent, int, error) {
		return h.d.Store.ListBTDeviceEvents(ctx, p, t, a, at, nil)
	})
}

// btGATT lists what operator-initiated GATT discovery found for one BLE
// device, newest discovery per item, built from the stored GATT events.
func (h *handlers) btGATT(eventType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, at, a, ok := btDeviceKey(w, r)
		if !ok {
			return
		}
		if t != storage.BTBLE {
			writeErr(w, http.StatusBadRequest, "invalid_parameter", "GATT applies to BLE devices only")
			return
		}
		if !h.btExists(w, r, t, a, at) {
			return
		}
		evs, total, err := h.d.Store.ListBTDeviceEvents(r.Context(), storage.Page{Limit: btGATTScan},
			t, a, at, []string{eventType})
		if err != nil {
			internal(w, h.d.Log, err)
			return
		}
		seen := map[string]bool{}
		items := []map[string]any{}
		for _, e := range evs { // newest first: the first of each key wins
			var env struct {
				Metadata map[string]any `json:"metadata"`
			}
			if err := json.Unmarshal(e.Payload, &env); err != nil || env.Metadata == nil {
				continue
			}
			m := env.Metadata
			key, _ := m["uuid"].(string)
			if eventType == events.TypeBtGattChar {
				svc, _ := m["service_uuid"].(string)
				key = svc + "/" + key + "/" + strconv.FormatFloat(numOr(m["handle"]), 'f', 0, 64)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			delete(m, "address")
			m["discovered_at"] = e.ReceivedAt
			m["event_id"] = e.EventID
			items = append(items, m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": items, "truncated": total > btGATTScan})
	}
}

func numOr(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return -1
}

// btExists writes 404 and returns false if the device is unknown.
func (h *handlers) btExists(w http.ResponseWriter, r *http.Request, t, a, at string) bool {
	_, err := h.d.Store.GetBTDevice(r.Context(), t, a, at)
	switch {
	case err == nil:
		return true
	case errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "resource not found")
	default:
		internal(w, h.d.Log, err)
	}
	return false
}

// btEvents is the event history restricted to bluetooth.* types; it takes
// the same filters as /events except bssid (a Wi-Fi field).
func (h *handlers) btEvents(w http.ResponseWriter, r *http.Request) {
	f, err := parseEventFilter(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}
	if f.BSSID != "" {
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "bssid does not apply to Bluetooth events")
		return
	}
	for _, t := range f.Types {
		if !strings.HasPrefix(t, "bluetooth.") {
			writeErr(w, http.StatusBadRequest, "invalid_parameter", "type must be a bluetooth.* event type")
			return
		}
	}
	f.TypePrefix = "bluetooth."
	paged(w, r, func(ctx context.Context, p storage.Page) ([]storage.StoredEvent, int, error) {
		return h.d.Store.ListEvents(ctx, p, f)
	})
}
