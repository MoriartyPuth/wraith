package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Tx is one ingestion transaction. All writes for one event happen in one Tx
// so the database never holds a half-applied event.
type Tx struct {
	tx *sql.Tx
	s  *Store
}

// WithTx runs fn in a transaction, committing on nil error.
func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Tx{tx: tx, s: s}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type stmts struct {
	insertEvent, touchSensor, upsertAP, insertObs, getAP *sql.Stmt
}

const (
	sqlInsertEvent = `INSERT INTO events(event_id, sensor_id, source, event_type, bssid, channel, sensor_ts, received_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(sensor_id, event_id) DO NOTHING`
	sqlTouchSensor = `INSERT INTO sensors(id, kind, first_seen, last_seen) VALUES (?1, ?2, ?3, ?3)
		ON CONFLICT(id) DO UPDATE SET last_seen = MAX(last_seen, ?3)`
	// NULL parameters mean "leave unchanged"; ?11 says whether rsn is being set.
	sqlUpsertAP = `INSERT INTO access_points(bssid, ssid, ssid_hex, first_seen, last_seen, last_channel, last_rssi, auth_mode, rsn, pmf, last_sensor)
		VALUES (?1, COALESCE(?2, ''), COALESCE(?3, ''), ?4, ?4, ?5, ?6, ?7, ?8, COALESCE(?9, 'unavailable'), ?10)
		ON CONFLICT(bssid) DO UPDATE SET
			ssid = COALESCE(?2, ssid), ssid_hex = COALESCE(?3, ssid_hex),
			last_seen = MAX(last_seen, ?4), last_channel = COALESCE(?5, last_channel),
			last_rssi = COALESCE(?6, last_rssi), auth_mode = COALESCE(?7, auth_mode),
			rsn = CASE WHEN ?11 THEN ?8 ELSE rsn END, pmf = COALESCE(?9, pmf), last_sensor = ?10`
	sqlInsertObs = `INSERT INTO observations(event_row, bssid, sensor_id, channel, rssi, observed_at) VALUES (?, ?, ?, ?, ?, ?)`
)

func (s *Store) prepare(ctx context.Context) error {
	var err error
	p := func(q string) *sql.Stmt {
		if err != nil {
			return nil
		}
		var st *sql.Stmt
		st, err = s.db.PrepareContext(ctx, q)
		return st
	}
	s.st = stmts{
		insertEvent: p(sqlInsertEvent),
		touchSensor: p(sqlTouchSensor),
		upsertAP:    p(sqlUpsertAP),
		insertObs:   p(sqlInsertObs),
		getAP:       p(`SELECT ` + apCols + ` FROM access_points WHERE bssid = ?`),
	}
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	return nil
}

func (t *Tx) stmt(s *sql.Stmt) *sql.Stmt { return t.tx.Stmt(s) }

// InsertEvent stores a validated event. inserted is false if the
// (sensor_id, event_id) pair already exists (a duplicate).
func (t *Tx) InsertEvent(ctx context.Context, e StoredEvent) (rowID int64, inserted bool, err error) {
	res, err := t.stmt(t.s.st.insertEvent).ExecContext(ctx, e.EventID, e.SensorID, e.Source, e.EventType,
		e.BSSID, e.Channel, e.SensorTS, e.ReceivedAt, string(e.Payload))
	if err != nil {
		return 0, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, false, nil
	}
	id, err := res.LastInsertId()
	return id, true, err
}

// TouchSensor creates the sensor if new and bumps last_seen.
func (t *Tx) TouchSensor(ctx context.Context, id, kind string, now int64) error {
	_, err := t.stmt(t.s.st.touchSensor).ExecContext(ctx, id, kind, now)
	return err
}

// SensorHeartbeat records a heartbeat.
func (t *Tx) SensorHeartbeat(ctx context.Context, id string, now int64, channel *int, bootID string) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE sensors SET last_heartbeat = ?, current_channel = ?, boot_id = ? WHERE id = ?`,
		now, channel, bootID, id)
	return err
}

// SensorStatus records a sensor.status report.
func (t *Tx) SensorStatus(ctx context.Context, id, state, fw, board string, caps []string, bootID string) error {
	if caps == nil {
		caps = []string{}
	}
	c, _ := json.Marshal(caps)
	_, err := t.tx.ExecContext(ctx, `UPDATE sensors SET state = ?, firmware_version = ?, board = NULLIF(?, ''),
		capabilities = ?, boot_id = ? WHERE id = ?`, state, fw, board, string(c), bootID, id)
	return err
}

// Sensor counters that IncSensorCounter may touch.
const (
	CounterRejected  = "rejected_count"
	CounterDuplicate = "duplicate_count"
	CounterGap       = "gap_count"
)

// IncSensorCounter adds n to one of the counters above. The sensor row is
// created if needed so rejections from unknown sensors are not lost.
func (s *Store) IncSensorCounter(ctx context.Context, id, kind, counter string, n int64, now int64) error {
	switch counter {
	case CounterRejected, CounterDuplicate, CounterGap:
	default:
		return fmt.Errorf("unknown counter %q", counter)
	}
	return s.WithTx(ctx, func(t *Tx) error {
		if err := t.TouchSensor(ctx, id, kind, now); err != nil {
			return err
		}
		// counter is one of three constants above, never user input.
		_, err := t.tx.ExecContext(ctx, `UPDATE sensors SET `+counter+` = `+counter+` + ? WHERE id = ?`, n, id)
		return err
	})
}

// APUpdate carries fields to merge into an AP row; nil means unchanged.
type APUpdate struct {
	BSSID    string
	SSID     *string
	SSIDHex  *string
	Seen     int64
	Channel  *int
	RSSI     *int
	AuthMode *string
	SetRSN   bool
	RSN      json.RawMessage // nil with SetRSN = "no RSN advertised"
	PMF      *string
	Sensor   string
}

// UpsertAP merges an observation into the AP inventory.
func (t *Tx) UpsertAP(ctx context.Context, u APUpdate) error {
	var rsn any
	if u.RSN != nil {
		rsn = string(u.RSN)
	}
	_, err := t.stmt(t.s.st.upsertAP).ExecContext(ctx, u.BSSID, u.SSID, u.SSIDHex, u.Seen, u.Channel, u.RSSI,
		u.AuthMode, rsn, u.PMF, u.Sensor, u.SetRSN)
	return err
}

// GetAP returns an AP inside the transaction (ErrNotFound if absent).
func (t *Tx) GetAP(ctx context.Context, bssid string) (AccessPoint, error) {
	a, err := scanAP(t.stmt(t.s.st.getAP).QueryRowContext(ctx, bssid))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// APsBySSIDHex returns all APs advertising exactly these SSID bytes.
func (t *Tx) APsBySSIDHex(ctx context.Context, hex string) ([]AccessPoint, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT `+apCols+` FROM access_points WHERE ssid_hex = ? ORDER BY first_seen`, hex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessPoint
	for rows.Next() {
		a, err := scanAP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// InsertObservation records one RSSI/channel sighting for history.
func (t *Tx) InsertObservation(ctx context.Context, eventRow int64, bssid, sensor string, channel, rssi *int, at int64) error {
	_, err := t.stmt(t.s.st.insertObs).ExecContext(ctx, eventRow, bssid, sensor, channel, rssi, at)
	return err
}

// InsertFinding stores a detection finding.
func (t *Tx) InsertFinding(ctx context.Context, f Finding) error {
	ev, _ := json.Marshal(f.Evidence)
	_, err := t.tx.ExecContext(ctx, `INSERT INTO findings(id, category, severity, confidence, classification, bssid,
		sensor_id, explanation, limitations, evidence, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
		f.ID, f.Category, f.Severity, f.Confidence, f.Classification, f.BSSID, f.SensorID,
		f.Explanation, f.Limitations, string(ev), f.CreatedAt, f.CreatedAt)
	return err
}

// FindingExistsSince reports whether an open or recent finding with the same
// category and BSSID was created at or after `since` (Unix ms). The pipeline
// uses this to suppress duplicate findings across Core restarts, where the
// detectors' in-memory dedup state has been lost.
func (s *Store) FindingExistsSince(ctx context.Context, category, bssid string, since int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM findings WHERE category = ? AND IFNULL(bssid,'') = ? AND created_at >= ?`,
		category, bssid, since).Scan(&n)
	return n > 0, err
}

// findingExistsSince is the transaction-scoped form used during ingestion.
func (t *Tx) FindingExistsSince(ctx context.Context, category, bssid string, since int64) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM findings WHERE category = ? AND IFNULL(bssid,'') = ? AND created_at >= ?`,
		category, bssid, since).Scan(&n)
	return n > 0, err
}

// ClientUpdate merges one aggregated station sighting into the clients table.
type ClientUpdate struct {
	BSSID   string
	Client  string
	Seen    int64
	Frames  int64
	RSSI    *int
	Channel *int
	Sensor  string
}

// UpsertClient records a client sighting. first_seen is preserved; frames
// accumulate; rssi/channel keep their last non-nil value.
func (t *Tx) UpsertClient(ctx context.Context, u ClientUpdate) error {
	if u.Frames < 1 {
		u.Frames = 1
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO clients
		(bssid, client, first_seen, last_seen, frames, last_rssi, last_channel, last_sensor)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bssid, client) DO UPDATE SET
			last_seen = excluded.last_seen,
			frames = clients.frames + excluded.frames,
			last_rssi = COALESCE(excluded.last_rssi, clients.last_rssi),
			last_channel = COALESCE(excluded.last_channel, clients.last_channel),
			last_sensor = excluded.last_sensor`,
		u.BSSID, u.Client, u.Seen, u.Seen, u.Frames, u.RSSI, u.Channel, u.Sensor)
	return err
}

// GetClient returns one sighting row inside the transaction.
func (t *Tx) GetClient(ctx context.Context, bssid, client string) (Client, error) {
	c, err := scanClient(t.tx.QueryRowContext(ctx, `SELECT `+clientCols+` FROM clients c
		LEFT JOIN access_points a ON a.bssid = c.bssid WHERE c.bssid = ? AND c.client = ?`, bssid, client))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}
