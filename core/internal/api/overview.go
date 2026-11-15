package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MoriartyPuth/wraith/core/internal/coverage"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

const (
	coverageDefault = 24 * time.Hour
	coverageMax     = 7 * 24 * time.Hour
	// A heartbeat comes every 5 s; four missed ones count as no data.
	coverageGapMS = 20_000
	searchPerKind = 8
)

// coverage reports, per sensor, what its radio was doing over a window:
// Wi-Fi, Bluetooth (± Classic inquiry) or no data. ?since=&until= in Unix
// ms; default the last 24 h, at most 7 days.
func (h *handlers) coverage(w http.ResponseWriter, r *http.Request) {
	now := h.d.Now().UnixMilli()
	until, since := now, now-coverageDefault.Milliseconds()
	q := r.URL.Query()
	for _, p := range []struct {
		name string
		dst  *int64
	}{{"since", &since}, {"until", &until}} {
		if v := q.Get(p.name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeErr(w, http.StatusBadRequest, "invalid_query", p.name+" must be Unix milliseconds")
				return
			}
			*p.dst = n
		}
	}
	if until > now {
		until = now // the future has no data yet, not "no data"
	}
	if until <= since || until-since > coverageMax.Milliseconds() {
		writeErr(w, http.StatusBadRequest, "invalid_query", "need since < until, at most 7 days apart")
		return
	}
	ctx := r.Context()
	sensors, _, err := h.d.Store.ListSensors(ctx, storage.Page{Limit: 100})
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	type sensorCov struct {
		SensorID string             `json:"sensor_id"`
		Segments []coverage.Segment `json:"segments"`
		TotalsMS map[string]int64   `json:"totals_ms"`
	}
	out := []sensorCov{}
	for _, s := range sensors {
		if s.LastSeen < since {
			continue // not active in the window
		}
		initial := coverage.Change{At: since, Mode: coverage.KindWifi}
		if st, ok, err := h.d.Store.RadioStateBefore(ctx, s.ID, since); err != nil {
			internal(w, h.d.Log, err)
			return
		} else if ok {
			initial = change(st)
		}
		states, err := h.d.Store.RadioStates(ctx, s.ID, since, until)
		if err != nil {
			internal(w, h.d.Log, err)
			return
		}
		changes := make([]coverage.Change, 0, len(states))
		for _, st := range states {
			changes = append(changes, change(st))
		}
		raw, err := h.d.Store.HeartbeatGaps(ctx, s.ID, since, until, coverageGapMS)
		if err != nil {
			internal(w, h.d.Log, err)
			return
		}
		gaps := make([]coverage.Gap, 0, len(raw))
		for _, g := range raw {
			gaps = append(gaps, coverage.Gap{From: g[0], To: g[1]})
		}
		segs := coverage.Build(since, until, initial, changes, gaps)
		out = append(out, sensorCov{SensorID: s.ID, Segments: segs, TotalsMS: coverage.Totals(segs)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"since": since, "until": until, "gap_threshold_ms": coverageGapMS, "sensors": out,
	}})
}

func change(st storage.RadioState) coverage.Change {
	return coverage.Change{At: st.At, Mode: st.Mode,
		Classic: st.State == "scanning_dual" || st.State == "scanning_classic"}
}

// search finds access points, clients and Bluetooth devices by name or
// address. ?q= 2-64 characters.
func (h *handlers) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 64 {
		writeErr(w, http.StatusBadRequest, "invalid_query", "q must be 2-64 characters")
		return
	}
	hits, err := h.d.Store.Search(r.Context(), q, searchPerKind)
	if err != nil {
		internal(w, h.d.Log, err)
		return
	}
	if hits == nil {
		hits = []storage.SearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": hits, "per_kind_limit": searchPerKind})
}
