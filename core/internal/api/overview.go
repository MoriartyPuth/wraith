package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MoriartyPuth/wraith/core/internal/btschedule"
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

// ---- Bluetooth schedule ----

// Scheduler is the daemon the handlers read and update (btschedule.Runner).
type Scheduler interface {
	Get() (cfg btschedule.Schedule, nextRun int64, nextOK, inSession bool)
	Set(ctx context.Context, cfg btschedule.Schedule) error
}

// btSchedule reports the unattended-session schedule, its next run and
// whether a scheduled session is running now.
func (h *handlers) btSchedule(w http.ResponseWriter, r *http.Request) {
	if h.d.Scheduler == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "the scheduler is not running")
		return
	}
	cfg, nextRun, nextOK, inSession := h.d.Scheduler.Get()
	writeJSON(w, http.StatusOK, map[string]any{"data": scheduleResponse(cfg, nextRun, nextOK, inSession)})
}

// btSetSchedule stores a new schedule. JSON-only (the same CSRF guard as /tx
// and /bluetooth/mode: a scheduled session pauses Wi-Fi monitoring).
func (h *handlers) btSetSchedule(w http.ResponseWriter, r *http.Request) {
	if h.d.Scheduler == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "the scheduler is not running")
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	var cfg btschedule.Schedule
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body",
			`expected {"enabled":bool,"every_minutes":int,"duration_seconds":int,"classic":bool}`)
		return
	}
	if err := h.d.Scheduler.Set(r.Context(), cfg); err != nil {
		if _, ok := err.(interface{ Error() string }); ok && isValidationErr(err) {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		internal(w, h.d.Log, err)
		return
	}
	stored, nextRun, nextOK, inSession := h.d.Scheduler.Get()
	writeJSON(w, http.StatusOK, map[string]any{"data": scheduleResponse(stored, nextRun, nextOK, inSession)})
}

func scheduleResponse(cfg btschedule.Schedule, nextRun int64, nextOK, inSession bool) map[string]any {
	m := map[string]any{
		"schedule":   cfg,
		"in_session": inSession,
		"limits": map[string]int{
			"min_every_minutes": btschedule.MinEveryMinutes, "max_every_minutes": btschedule.MaxEveryMinutes,
			"min_duration_seconds": btschedule.MinDurationSeconds, "max_duration_seconds": btschedule.MaxDurationSeconds,
		},
	}
	if nextOK {
		m["next_run"] = nextRun
	}
	return m
}

// isValidationErr distinguishes a bad-schedule error (client's fault) from a
// storage error. Validation errors come only from Schedule.Validate.
func isValidationErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "every_minutes") || strings.Contains(s, "duration_seconds")
}
