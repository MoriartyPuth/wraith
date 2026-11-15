package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ---- coverage inputs (internal/coverage builds the timeline) ----

// RadioState is a bluetooth.system.status report: the sensor's radio mode
// and scan state at At (Unix ms).
type RadioState struct {
	At    int64
	Mode  string
	State string
}

const radioStateCols = `received_at, IFNULL(json_extract(payload, '$.metadata.mode'), ''),
	IFNULL(json_extract(payload, '$.metadata.state'), '')`

// RadioStateBefore returns the last radio report of a sensor before `before`
// (ok=false if it never sent one).
func (s *Store) RadioStateBefore(ctx context.Context, sensorID string, before int64) (RadioState, bool, error) {
	var r RadioState
	err := s.db.QueryRowContext(ctx, `SELECT `+radioStateCols+` FROM events
		WHERE event_type = 'bluetooth.system.status' AND sensor_id = ? AND received_at < ?
		ORDER BY received_at DESC LIMIT 1`, sensorID, before).Scan(&r.At, &r.Mode, &r.State)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

// RadioStates returns a sensor's radio reports in [since, until), oldest first.
func (s *Store) RadioStates(ctx context.Context, sensorID string, since, until int64) ([]RadioState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+radioStateCols+` FROM events
		WHERE event_type = 'bluetooth.system.status' AND sensor_id = ? AND received_at >= ? AND received_at < ?
		ORDER BY received_at`, sensorID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RadioState
	for rows.Next() {
		var r RadioState
		if err := rows.Scan(&r.At, &r.Mode, &r.State); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HeartbeatGaps returns the intervals in [since, until) longer than maxGap
// (ms) without a sensor.heartbeat from the sensor, including before the
// first and after the last heartbeat of the window. No heartbeat at all =
// the whole window. Heartbeats come every 5 s from the firmware; they stop
// when the sensor is offline, the link is down or Core is not running.
func (s *Store) HeartbeatGaps(ctx context.Context, sensorID string, since, until, maxGap int64) ([][2]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT received_at FROM events
		WHERE event_type = 'sensor.heartbeat' AND sensor_id = ? AND received_at >= ? AND received_at < ?
		ORDER BY received_at`, sensorID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]int64
	prev := since
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if t-prev > maxGap {
			out = append(out, [2]int64{prev, t})
		}
		prev = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if until-prev > maxGap {
		out = append(out, [2]int64{prev, until})
	}
	return out, nil
}

// ---- global search ----

// SearchHit is one match of a global search.
type SearchHit struct {
	Kind     string `json:"kind"` // "access_point" | "client" | "bt_device"
	ID       string `json:"id"`   // BSSID, client MAC, or BTDeviceID
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	LastSeen int64  `json:"last_seen"`
}

var reAddrQuery = regexp.MustCompile(`^[0-9A-Fa-f:.\-]+$`)

// likeEscape makes s a literal LIKE pattern fragment (escape char '\').
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Search finds access points (SSID or BSSID), client MACs and Bluetooth
// devices (name or address) matching q, newest first, at most perKind each.
// Address queries ignore separators: "70b2a8", "70:B2:A8" and "70-b2-a8" all
// match "…:70:B2:A8".
func (s *Store) Search(ctx context.Context, q string, perKind int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	text := "%" + likeEscape(strings.ToLower(q)) + "%"
	addr := "\x00" // matches nothing unless q looks like (part of) an address
	if reAddrQuery.MatchString(q) {
		h := strings.NewReplacer(":", "", "-", "", ".", "").Replace(strings.ToUpper(q))
		if len(h) >= 2 {
			addr = "%" + h + "%"
		}
	}
	var out []SearchHit
	run := func(query string, args []any, scan func(*sql.Rows) (SearchHit, error)) error {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	}
	err := run(`SELECT bssid, ssid, IFNULL(auth_mode, ''), IFNULL(last_channel, 0), last_seen FROM access_points
		WHERE LOWER(ssid) LIKE ? ESCAPE '\' OR REPLACE(bssid, ':', '') LIKE ?
		ORDER BY last_seen DESC LIMIT ?`, []any{text, addr, perKind},
		func(r *sql.Rows) (SearchHit, error) {
			var h SearchHit
			var ssid, auth string
			var ch int
			err := r.Scan(&h.ID, &ssid, &auth, &ch, &h.LastSeen)
			h.Kind, h.Title = "access_point", ssid
			if ssid == "" {
				h.Title = "(hidden SSID)"
			}
			h.Detail = h.ID
			if auth != "" {
				h.Detail += " · " + auth
			}
			return h, err
		})
	if err != nil {
		return nil, err
	}
	if addr != "\x00" {
		err = run(`SELECT c.client, COUNT(*), MAX(c.last_seen),
				IFNULL((SELECT COALESCE(NULLIF(a.ssid, ''), c2.bssid) FROM clients c2 LEFT JOIN access_points a ON a.bssid = c2.bssid
				        WHERE c2.client = c.client ORDER BY c2.last_seen DESC LIMIT 1), '')
			FROM clients c WHERE REPLACE(c.client, ':', '') LIKE ?
			GROUP BY c.client ORDER BY MAX(c.last_seen) DESC LIMIT ?`, []any{addr, perKind},
			func(r *sql.Rows) (SearchHit, error) {
				var h SearchHit
				var n int
				var ssid string
				err := r.Scan(&h.ID, &n, &h.LastSeen, &ssid)
				h.Kind, h.Title = "client", h.ID
				h.Detail = "client"
				if ssid != "" {
					h.Detail += " · last with " + ssid
				}
				if n > 1 {
					h.Detail += " · " + strconv.Itoa(n) + " APs"
				}
				return h, err
			})
		if err != nil {
			return nil, err
		}
	}
	err = run(`SELECT transport, address_type, address, IFNULL(name, ''), IFNULL(device_class, IFNULL(classification, '')), last_seen
		FROM bt_devices WHERE LOWER(IFNULL(name, '')) LIKE ? ESCAPE '\' OR REPLACE(address, ':', '') LIKE ?
		ORDER BY last_seen DESC LIMIT ?`, []any{text, addr, perKind},
		func(r *sql.Rows) (SearchHit, error) {
			var h SearchHit
			var transport, at, address, name, class string
			err := r.Scan(&transport, &at, &address, &name, &class, &h.LastSeen)
			h.Kind, h.ID = "bt_device", BTDeviceID(transport, at, address)
			h.Title = name
			if name == "" {
				h.Title = address
			}
			h.Detail = address + " · " + map[string]string{BTClassic: "Classic", BTBLE: "BLE"}[transport]
			if at != "" {
				h.Detail += " " + at
			}
			if class != "" {
				h.Detail += " · " + class
			}
			return h, err
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- app_config (key/value settings) ----

// GetConfig returns the value stored under key, or ("", false) if unset.
func (s *Store) GetConfig(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_config WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetConfig stores value under key (insert or replace).
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_config(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// AnySensorHasCapability reports whether any known sensor announced capability c.
func (s *Store) AnySensorHasCapability(ctx context.Context, c string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT capabilities FROM sensors`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var caps string
		if err := rows.Scan(&caps); err != nil {
			return false, err
		}
		var list []string
		if json.Unmarshal([]byte(caps), &list) == nil {
			for _, x := range list {
				if x == c {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}

// AnyRadioMode reports whether any sensor's last Bluetooth status had this
// radio mode ("wifi" | "bluetooth").
func (s *Store) AnyRadioMode(ctx context.Context, mode string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bt_sensor_status WHERE mode = ?`, mode).Scan(&n)
	return n > 0, err
}
