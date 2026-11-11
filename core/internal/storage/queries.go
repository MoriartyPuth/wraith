package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Page is a pagination request. Limit is clamped by the API layer.
type Page struct {
	Limit  int
	Offset int
}

// Sensor is a row of the sensors table.
type Sensor struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	FirmwareVersion *string         `json:"firmware_version"`
	Board           *string         `json:"board"`
	State           string          `json:"state"`
	Capabilities    json.RawMessage `json:"capabilities"`
	BootID          *string         `json:"boot_id"`
	FirstSeen       int64           `json:"first_seen"`
	LastSeen        int64           `json:"last_seen"`
	LastHeartbeat   *int64          `json:"last_heartbeat"`
	CurrentChannel  *int            `json:"current_channel"`
	RejectedCount   int64           `json:"rejected_count"`
	DuplicateCount  int64           `json:"duplicate_count"`
	GapCount        int64           `json:"gap_count"`
}

const sensorCols = `id, kind, firmware_version, board, state, capabilities, boot_id, first_seen,
	last_seen, last_heartbeat, current_channel, rejected_count, duplicate_count, gap_count`

func scanSensor(sc interface{ Scan(...any) error }) (Sensor, error) {
	var s Sensor
	var caps string
	err := sc.Scan(&s.ID, &s.Kind, &s.FirmwareVersion, &s.Board, &s.State, &caps, &s.BootID,
		&s.FirstSeen, &s.LastSeen, &s.LastHeartbeat, &s.CurrentChannel,
		&s.RejectedCount, &s.DuplicateCount, &s.GapCount)
	s.Capabilities = json.RawMessage(caps)
	return s, err
}

// ListSensors returns sensors ordered by last_seen descending, and the total.
func (s *Store) ListSensors(ctx context.Context, p Page) ([]Sensor, int, error) {
	return list(ctx, s.db, `sensors`, sensorCols, `last_seen DESC`, p, scanSensor)
}

// GetSensor returns one sensor.
func (s *Store) GetSensor(ctx context.Context, id string) (Sensor, error) {
	return get(ctx, s.db, `SELECT `+sensorCols+` FROM sensors WHERE id = ?`, id, scanSensor)
}

// AccessPoint is a row of the access_points table.
type AccessPoint struct {
	BSSID       string          `json:"bssid"`
	SSID        string          `json:"ssid"`
	SSIDHex     string          `json:"ssid_hex"`
	FirstSeen   int64           `json:"first_seen"`
	LastSeen    int64           `json:"last_seen"`
	LastChannel *int            `json:"last_channel"`
	LastRSSI    *int            `json:"last_rssi"`
	AuthMode    *string         `json:"auth_mode"`
	RSN         json.RawMessage `json:"rsn"`
	PMF         string          `json:"pmf"`
	LastSensor  *string         `json:"last_sensor"`
}

const apCols = `bssid, ssid, ssid_hex, first_seen, last_seen, last_channel, last_rssi, auth_mode, rsn, pmf, last_sensor`

func scanAP(sc interface{ Scan(...any) error }) (AccessPoint, error) {
	var a AccessPoint
	var rsn sql.NullString
	err := sc.Scan(&a.BSSID, &a.SSID, &a.SSIDHex, &a.FirstSeen, &a.LastSeen, &a.LastChannel,
		&a.LastRSSI, &a.AuthMode, &rsn, &a.PMF, &a.LastSensor)
	if rsn.Valid {
		a.RSN = json.RawMessage(rsn.String)
	} else {
		a.RSN = json.RawMessage("null")
	}
	return a, err
}

// ListAccessPoints returns APs ordered by last_seen descending.
func (s *Store) ListAccessPoints(ctx context.Context, p Page) ([]AccessPoint, int, error) {
	return list(ctx, s.db, `access_points`, apCols, `last_seen DESC`, p, scanAP)
}

// GetAccessPoint returns one AP by BSSID.
func (s *Store) GetAccessPoint(ctx context.Context, bssid string) (AccessPoint, error) {
	return get(ctx, s.db, `SELECT `+apCols+` FROM access_points WHERE bssid = ?`, bssid, scanAP)
}

// StoredEvent is a row of the events table.
type StoredEvent struct {
	ID         int64           `json:"id"`
	EventID    string          `json:"event_id"`
	SensorID   string          `json:"sensor_id"`
	Source     string          `json:"source"`
	EventType  string          `json:"event_type"`
	BSSID      *string         `json:"bssid"`
	Channel    *int            `json:"channel"`
	SensorTS   int64           `json:"sensor_ts"`
	ReceivedAt int64           `json:"received_at"`
	Payload    json.RawMessage `json:"payload"`
}

const eventCols = `id, event_id, sensor_id, source, event_type, bssid, channel, sensor_ts, received_at, payload`

func scanEvent(sc interface{ Scan(...any) error }) (StoredEvent, error) {
	var e StoredEvent
	var payload string
	err := sc.Scan(&e.ID, &e.EventID, &e.SensorID, &e.Source, &e.EventType, &e.BSSID,
		&e.Channel, &e.SensorTS, &e.ReceivedAt, &payload)
	e.Payload = json.RawMessage(payload)
	return e, err
}

// EventFilter narrows ListEvents. Zero values mean "any".
type EventFilter struct {
	EventIDs []string
	Types    []string
	BSSID    string
	SensorID string
	Since    int64 // received_at >= Since (ms)
	Until    int64 // received_at <  Until (ms)
}

func (f EventFilter) where() (string, []any) {
	var conds []string
	var args []any
	if len(f.Types) > 0 {
		ph := make([]string, len(f.Types))
		for i, t := range f.Types {
			ph[i] = "?"
			args = append(args, t)
		}
		conds = append(conds, "event_type IN ("+strings.Join(ph, ",")+")")
	}
	if len(f.EventIDs) > 0 {
		ph := make([]string, len(f.EventIDs))
		for i, id := range f.EventIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		conds = append(conds, "event_id IN ("+strings.Join(ph, ",")+")")
	}
	if f.BSSID != "" {
		conds, args = append(conds, "bssid = ?"), append(args, f.BSSID)
	}
	if f.SensorID != "" {
		conds, args = append(conds, "sensor_id = ?"), append(args, f.SensorID)
	}
	if f.Since > 0 {
		conds, args = append(conds, "received_at >= ?"), append(args, f.Since)
	}
	if f.Until > 0 {
		conds, args = append(conds, "received_at < ?"), append(args, f.Until)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListEvents returns events newest first, optionally filtered.
func (s *Store) ListEvents(ctx context.Context, p Page, f EventFilter) ([]StoredEvent, int, error) {
	where, args := f.where()
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM events`+where+
		` ORDER BY received_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []StoredEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Observation is one RSSI/channel sighting of an AP.
type Observation struct {
	SensorID   string `json:"sensor_id"`
	Channel    *int   `json:"channel"`
	RSSI       *int   `json:"rssi"`
	ObservedAt int64  `json:"observed_at"`
}

// ListObservations returns the newest observations of an AP.
func (s *Store) ListObservations(ctx context.Context, bssid string, limit int) ([]Observation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sensor_id, channel, rssi, observed_at FROM observations
		WHERE bssid = ? ORDER BY observed_at DESC, id DESC LIMIT ?`, bssid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Observation{}
	for rows.Next() {
		var o Observation
		if err := rows.Scan(&o.SensorID, &o.Channel, &o.RSSI, &o.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetFindingStatus changes a finding's review status.
func (s *Store) SetFindingStatus(ctx context.Context, id, status string, now int64) (Finding, error) {
	switch status {
	case "open", "reviewed", "dismissed":
	default:
		return Finding{}, fmt.Errorf("invalid status %q", status)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE findings SET status = ?, updated_at = ? WHERE id = ?`, status, now, id)
	if err != nil {
		return Finding{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Finding{}, ErrNotFound
	}
	return s.GetFinding(ctx, id)
}

// GetEvent returns one event by row id.
func (s *Store) GetEvent(ctx context.Context, id int64) (StoredEvent, error) {
	return get(ctx, s.db, `SELECT `+eventCols+` FROM events WHERE id = ?`, id, scanEvent)
}

// Finding is a row of the findings table.
type Finding struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Confidence     float64  `json:"confidence"`
	Classification string   `json:"classification"`
	BSSID          *string  `json:"bssid"`
	SensorID       *string  `json:"sensor_id"`
	Explanation    string   `json:"explanation"`
	Limitations    string   `json:"limitations"`
	Evidence       []string `json:"evidence"`
	Status         string   `json:"status"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

const findingCols = `id, category, severity, confidence, classification, bssid, sensor_id, explanation, limitations, evidence, status, created_at, updated_at`

func scanFinding(sc interface{ Scan(...any) error }) (Finding, error) {
	var f Finding
	var ev string
	err := sc.Scan(&f.ID, &f.Category, &f.Severity, &f.Confidence, &f.Classification, &f.BSSID,
		&f.SensorID, &f.Explanation, &f.Limitations, &ev, &f.Status, &f.CreatedAt, &f.UpdatedAt)
	if err == nil {
		if jerr := json.Unmarshal([]byte(ev), &f.Evidence); jerr != nil {
			f.Evidence = []string{}
		}
	}
	if f.Evidence == nil {
		f.Evidence = []string{}
	}
	return f, err
}

// ListFindings returns findings newest first.
func (s *Store) ListFindings(ctx context.Context, p Page) ([]Finding, int, error) {
	return list(ctx, s.db, `findings`, findingCols, `created_at DESC`, p, scanFinding)
}

// GetFinding returns one finding.
func (s *Store) GetFinding(ctx context.Context, id string) (Finding, error) {
	return get(ctx, s.db, `SELECT `+findingCols+` FROM findings WHERE id = ?`, id, scanFinding)
}

// Stats are aggregate counts for /statistics.
type Stats struct {
	Sensors      int            `json:"sensors"`
	AccessPoints int            `json:"access_points"`
	Events       int            `json:"events"`
	Findings     int            `json:"findings"`
	OpenFindings int            `json:"open_findings"`
	APsByChannel map[int]int    `json:"aps_by_channel"`
	EventsByType map[string]int `json:"events_by_type"`
}

// Statistics computes aggregate counts.
func (s *Store) Statistics(ctx context.Context) (Stats, error) {
	st := Stats{APsByChannel: map[int]int{}, EventsByType: map[string]int{}}
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM sensors), (SELECT COUNT(*) FROM access_points),
		(SELECT COUNT(*) FROM events), (SELECT COUNT(*) FROM findings),
		(SELECT COUNT(*) FROM findings WHERE status = 'open')`).
		Scan(&st.Sensors, &st.AccessPoints, &st.Events, &st.Findings, &st.OpenFindings)
	if err != nil {
		return st, err
	}
	if err := groupCount(ctx, s.db, `SELECT last_channel, COUNT(*) FROM access_points WHERE last_channel IS NOT NULL GROUP BY last_channel`, st.APsByChannel); err != nil {
		return st, err
	}
	err = groupCount(ctx, s.db, `SELECT event_type, COUNT(*) FROM events GROUP BY event_type`, st.EventsByType)
	return st, err
}

func groupCount[K comparable](ctx context.Context, db *sql.DB, q string, into map[K]int) error {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k K
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		into[k] = n
	}
	return rows.Err()
}

// list runs a paginated SELECT. table, cols and order are compile-time
// constants from this package, never user input.
func list[T any](ctx context.Context, db *sql.DB, table, cols, order string, p Page,
	scan func(interface{ Scan(...any) error }) (T, error)) ([]T, int, error) {
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT `+cols+` FROM `+table+` ORDER BY `+order+` LIMIT ? OFFSET ?`, p.Limit, p.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func get[T any](ctx context.Context, db *sql.DB, q string, arg any,
	scan func(interface{ Scan(...any) error }) (T, error)) (T, error) {
	v, err := scan(db.QueryRowContext(ctx, q, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
