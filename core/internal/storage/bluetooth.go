package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Bluetooth transports (bt_devices.transport).
const (
	BTClassic = "classic"
	BTBLE     = "ble"
)

// BTDevice is a row of bt_devices: one observed Bluetooth address. Fields the
// sensor never reported stay nil — the dashboard shows them as unavailable,
// never as guessed values.
type BTDevice struct {
	ID             string   `json:"id"` // BTDeviceID(transport, address_type, address)
	Transport      string   `json:"transport"`
	Address        string   `json:"address"`
	AddressType    string   `json:"address_type"` // "" for Classic
	Name           *string  `json:"name"`
	CoD            *int     `json:"cod"`
	DeviceClass    *string  `json:"device_class"`   // Classic, decoded from CoD
	Classification *string  `json:"classification"` // BLE, inferred
	Confidence     *float64 `json:"confidence"`
	ManufacturerID *int     `json:"manufacturer_id"`
	TxPower        *int     `json:"tx_power"`
	Services       []string `json:"services"`
	LastRSSI       *int     `json:"last_rssi"`
	RSSIAvg        *float64 `json:"rssi_avg"`
	Observations   int64    `json:"observations"`
	FirstSeen      int64    `json:"first_seen"`
	LastSeen       int64    `json:"last_seen"`
	LastSensor     *string  `json:"last_sensor"`
}

// BTDeviceID is the stable, URL-safe identifier of a device row:
// "classic:AA:BB:CC:DD:EE:FF" or "ble:<public|random>:AA:BB:CC:DD:EE:FF".
func BTDeviceID(transport, addrType, address string) string {
	if transport == BTClassic {
		return BTClassic + ":" + address
	}
	return transport + ":" + addrType + ":" + address
}

// ParseBTDeviceID splits an id from BTDeviceID. It checks the shape only;
// callers validate the address format.
func ParseBTDeviceID(id string) (transport, addrType, address string, ok bool) {
	switch {
	case strings.HasPrefix(id, BTClassic+":"):
		return BTClassic, "", strings.TrimPrefix(id, BTClassic+":"), true
	case strings.HasPrefix(id, BTBLE+":public:"):
		return BTBLE, "public", strings.TrimPrefix(id, BTBLE+":public:"), true
	case strings.HasPrefix(id, BTBLE+":random:"):
		return BTBLE, "random", strings.TrimPrefix(id, BTBLE+":random:"), true
	}
	return "", "", "", false
}

// BTObservation is a row of bt_observations.
type BTObservation struct {
	EventRow    int64  `json:"event_row"`
	Transport   string `json:"transport"`
	Address     string `json:"address"`
	AddressType string `json:"address_type"`
	EventType   string `json:"event_type"`
	RSSI        *int   `json:"rssi"`
	SensorID    string `json:"sensor_id"`
	ObservedAt  int64  `json:"observed_at"`
}

// BTSensorStatus is a row of bt_sensor_status.
type BTSensorStatus struct {
	SensorID         string  `json:"sensor_id"`
	Mode             *string `json:"mode"`
	State            *string `json:"state"`
	ClassicDevices   *int64  `json:"classic_devices"`
	BLEDevices       *int64  `json:"ble_devices"`
	Message          *string `json:"message"`
	UpdatedAt        *int64  `json:"updated_at"`
	ErrorCount       int64   `json:"error_count"`
	LastErrorCode    *string `json:"last_error_code"`
	LastErrorMessage *string `json:"last_error_message"`
	LastErrorAt      *int64  `json:"last_error_at"`
}

const btDeviceCols = `transport, address, address_type, name, cod, device_class, classification,
	confidence, manufacturer_id, tx_power, services, last_rssi, rssi_avg, observations,
	first_seen, last_seen, last_sensor`

func scanBTDevice(sc interface{ Scan(...any) error }) (BTDevice, error) {
	var d BTDevice
	var services string
	err := sc.Scan(&d.Transport, &d.Address, &d.AddressType, &d.Name, &d.CoD, &d.DeviceClass,
		&d.Classification, &d.Confidence, &d.ManufacturerID, &d.TxPower, &services, &d.LastRSSI,
		&d.RSSIAvg, &d.Observations, &d.FirstSeen, &d.LastSeen, &d.LastSensor)
	if err != nil {
		return d, err
	}
	d.ID = BTDeviceID(d.Transport, d.AddressType, d.Address)
	d.Services = []string{}
	if services != "" {
		if err := json.Unmarshal([]byte(services), &d.Services); err != nil {
			return d, err
		}
	}
	return d, nil
}

func getBTDevice(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, transport, address, addrType string) (BTDevice, error) {
	d, err := scanBTDevice(q.QueryRowContext(ctx, `SELECT `+btDeviceCols+` FROM bt_devices
		WHERE transport = ? AND address = ? AND address_type = ?`, transport, address, addrType))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// GetBTDevice returns one device row inside an ingest transaction.
func (t *Tx) GetBTDevice(ctx context.Context, transport, address, addrType string) (BTDevice, error) {
	return getBTDevice(ctx, t.tx, transport, address, addrType)
}

// GetBTDevice returns one device row.
func (s *Store) GetBTDevice(ctx context.Context, transport, address, addrType string) (BTDevice, error) {
	return getBTDevice(ctx, s.db, transport, address, addrType)
}

// PutBTDevice writes a fully merged device row (the merge happens in
// internal/bluetooth, inside the same ingest transaction, so the
// read-modify-write is atomic with the event insert).
func (t *Tx) PutBTDevice(ctx context.Context, d BTDevice) error {
	if d.Services == nil {
		d.Services = []string{}
	}
	services, err := json.Marshal(d.Services)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO bt_devices (`+btDeviceCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(transport, address, address_type) DO UPDATE SET
			name = excluded.name, cod = excluded.cod, device_class = excluded.device_class,
			classification = excluded.classification, confidence = excluded.confidence,
			manufacturer_id = excluded.manufacturer_id, tx_power = excluded.tx_power,
			services = excluded.services, last_rssi = excluded.last_rssi, rssi_avg = excluded.rssi_avg,
			observations = excluded.observations, last_seen = excluded.last_seen,
			last_sensor = excluded.last_sensor`,
		d.Transport, d.Address, d.AddressType, d.Name, d.CoD, d.DeviceClass, d.Classification,
		d.Confidence, d.ManufacturerID, d.TxPower, string(services), d.LastRSSI, d.RSSIAvg,
		d.Observations, d.FirstSeen, d.LastSeen, d.LastSensor)
	return err
}

// InsertBTObservation links a stored event to a device address.
func (t *Tx) InsertBTObservation(ctx context.Context, o BTObservation) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO bt_observations
		(event_row, transport, address, address_type, event_type, rssi, sensor_id, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.EventRow, o.Transport, o.Address, o.AddressType, o.EventType, o.RSSI, o.SensorID, o.ObservedAt)
	return err
}

// BTStatusUpdate is the content of a bluetooth.system.status event.
type BTStatusUpdate struct {
	SensorID       string
	Mode, State    string
	ClassicDevices *int64
	BLEDevices     *int64
	Message        *string
	At             int64
}

// UpsertBTStatus records the latest Bluetooth subsystem state of a sensor.
// The error tally is preserved.
func (t *Tx) UpsertBTStatus(ctx context.Context, u BTStatusUpdate) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO bt_sensor_status
		(sensor_id, mode, state, classic_devices, ble_devices, message, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sensor_id) DO UPDATE SET
			mode = excluded.mode, state = excluded.state,
			classic_devices = excluded.classic_devices, ble_devices = excluded.ble_devices,
			message = excluded.message, updated_at = excluded.updated_at`,
		u.SensorID, u.Mode, u.State, u.ClassicDevices, u.BLEDevices, u.Message, u.At)
	return err
}

// RecordBTError counts a bluetooth.system.error and keeps the latest one.
func (t *Tx) RecordBTError(ctx context.Context, sensorID, code, message string, at int64) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO bt_sensor_status
		(sensor_id, error_count, last_error_code, last_error_message, last_error_at)
		VALUES (?, 1, ?, ?, ?)
		ON CONFLICT(sensor_id) DO UPDATE SET
			error_count = bt_sensor_status.error_count + 1,
			last_error_code = excluded.last_error_code,
			last_error_message = excluded.last_error_message,
			last_error_at = excluded.last_error_at`,
		sensorID, code, message, at)
	return err
}

// BTDeviceFilter narrows ListBTDevices. Empty fields match everything.
type BTDeviceFilter struct {
	Transport string // "classic" | "ble" | ""
	Since     int64  // last_seen >= Since (Unix ms) when > 0
}

// ListBTDevices returns devices newest first.
func (s *Store) ListBTDevices(ctx context.Context, p Page, f BTDeviceFilter) ([]BTDevice, int, error) {
	where := " WHERE 1=1"
	var args []any
	if f.Transport != "" {
		where += " AND transport = ?"
		args = append(args, f.Transport)
	}
	if f.Since > 0 {
		where += " AND last_seen >= ?"
		args = append(args, f.Since)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bt_devices`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+btDeviceCols+` FROM bt_devices`+where+
		` ORDER BY last_seen DESC, address LIMIT ? OFFSET ?`, append(args, p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []BTDevice{}
	for rows.Next() {
		d, err := scanBTDevice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// ListBTObservations returns a device's observations, newest first.
func (s *Store) ListBTObservations(ctx context.Context, transport, address, addrType string, limit int) ([]BTObservation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_row, transport, address, address_type, event_type,
		rssi, sensor_id, observed_at FROM bt_observations
		WHERE transport = ? AND address = ? AND address_type = ?
		ORDER BY observed_at DESC, event_row DESC LIMIT ?`, transport, address, addrType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BTObservation{}
	for rows.Next() {
		var o BTObservation
		if err := rows.Scan(&o.EventRow, &o.Transport, &o.Address, &o.AddressType, &o.EventType,
			&o.RSSI, &o.SensorID, &o.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListBTStatus returns the Bluetooth subsystem state of every sensor that has
// reported one.
func (s *Store) ListBTStatus(ctx context.Context) ([]BTSensorStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sensor_id, mode, state, classic_devices, ble_devices,
		message, updated_at, error_count, last_error_code, last_error_message, last_error_at
		FROM bt_sensor_status ORDER BY sensor_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BTSensorStatus{}
	for rows.Next() {
		var b BTSensorStatus
		if err := rows.Scan(&b.SensorID, &b.Mode, &b.State, &b.ClassicDevices, &b.BLEDevices,
			&b.Message, &b.UpdatedAt, &b.ErrorCount, &b.LastErrorCode, &b.LastErrorMessage,
			&b.LastErrorAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// LatestBLEAddressType returns the address_type of the most recently seen BLE
// device row with this address, or "" if there is none.
func (t *Tx) LatestBLEAddressType(ctx context.Context, address string) (string, error) {
	var at string
	err := t.tx.QueryRowContext(ctx, `SELECT address_type FROM bt_devices
		WHERE transport = 'ble' AND address = ? ORDER BY last_seen DESC LIMIT 1`, address).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return at, err
}

// GetBTStatus returns one sensor's Bluetooth subsystem state.
func (s *Store) GetBTStatus(ctx context.Context, sensorID string) (BTSensorStatus, error) {
	var b BTSensorStatus
	err := s.db.QueryRowContext(ctx, `SELECT sensor_id, mode, state, classic_devices, ble_devices,
		message, updated_at, error_count, last_error_code, last_error_message, last_error_at
		FROM bt_sensor_status WHERE sensor_id = ?`, sensorID).Scan(&b.SensorID, &b.Mode, &b.State,
		&b.ClassicDevices, &b.BLEDevices, &b.Message, &b.UpdatedAt, &b.ErrorCount,
		&b.LastErrorCode, &b.LastErrorMessage, &b.LastErrorAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// ListBTDeviceEvents returns the stored events linked to one device through
// bt_observations, newest first, optionally limited to some event types.
func (s *Store) ListBTDeviceEvents(ctx context.Context, p Page, transport, address, addrType string, types []string) ([]StoredEvent, int, error) {
	where := ` FROM events e JOIN bt_observations o ON o.event_row = e.id
		WHERE o.transport = ? AND o.address = ? AND o.address_type = ?`
	args := []any{transport, address, addrType}
	if len(types) > 0 {
		where += " AND e.event_type IN (?" + strings.Repeat(",?", len(types)-1) + ")"
		for _, t := range types {
			args = append(args, t)
		}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	cols := "e." + strings.ReplaceAll(eventCols, ", ", ", e.")
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+where+` ORDER BY e.id DESC LIMIT ? OFFSET ?`,
		append(args, p.Limit, p.Offset)...)
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
