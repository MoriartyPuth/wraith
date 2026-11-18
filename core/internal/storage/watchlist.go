package storage

import (
	"context"
	"database/sql"
	"errors"
)

// WatchEntry is one row of bt_watchlist: a Bluetooth address the operator
// chose to track. Presence is derived (see internal/presence); Present and
// StateSince hold the last evaluated transition, which is what lets the
// arrival/departure evaluator resume correctly after a Core restart.
type WatchEntry struct {
	ID          string `json:"id"`
	Transport   string `json:"transport"`
	Address     string `json:"address"`
	AddressType string `json:"address_type"`
	Label       string `json:"label"`
	AddedAt     int64  `json:"added_at"`
	Present     bool   `json:"present"`
	StateSince  *int64 `json:"state_since"`
	LastEvalAt  *int64 `json:"last_eval_at"`
}

const watchCols = `transport, address, address_type, label, added_at, present, state_since, last_eval_at`

func scanWatch(sc interface{ Scan(...any) error }) (WatchEntry, error) {
	var w WatchEntry
	var present int
	err := sc.Scan(&w.Transport, &w.Address, &w.AddressType, &w.Label, &w.AddedAt,
		&present, &w.StateSince, &w.LastEvalAt)
	w.Present = present != 0
	w.ID = BTDeviceID(w.Transport, w.AddressType, w.Address)
	return w, err
}

// ListWatchlist returns watched addresses, present ones first.
func (s *Store) ListWatchlist(ctx context.Context) ([]WatchEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+watchCols+` FROM bt_watchlist
		ORDER BY present DESC, added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WatchEntry{}
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWatch returns one watchlist row (ErrNotFound if absent).
func (s *Store) GetWatch(ctx context.Context, transport, address, addrType string) (WatchEntry, error) {
	w, err := scanWatch(s.db.QueryRowContext(ctx, `SELECT `+watchCols+` FROM bt_watchlist
		WHERE transport = ? AND address = ? AND address_type = ?`, transport, address, addrType))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

// PutWatch inserts a watchlist row, or updates its label if it already
// exists (presence state is preserved on update). The caller sets the initial
// Present so adding an already-present device does not raise a false arrival.
func (s *Store) PutWatch(ctx context.Context, w WatchEntry) error {
	present := 0
	if w.Present {
		present = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO bt_watchlist
		(transport, address, address_type, label, added_at, present, state_since, last_eval_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(transport, address, address_type) DO UPDATE SET label = excluded.label`,
		w.Transport, w.Address, w.AddressType, w.Label, w.AddedAt, present, w.StateSince, w.LastEvalAt)
	return err
}

// DeleteWatch removes a watchlist row (ErrNotFound if absent).
func (s *Store) DeleteWatch(ctx context.Context, transport, address, addrType string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM bt_watchlist
		WHERE transport = ? AND address = ? AND address_type = ?`, transport, address, addrType)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetWatchPresence records an evaluated presence transition.
func (s *Store) SetWatchPresence(ctx context.Context, transport, address, addrType string,
	present bool, stateSince, evalAt int64) error {
	p := 0
	if present {
		p = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE bt_watchlist
		SET present = ?, state_since = ?, last_eval_at = ?
		WHERE transport = ? AND address = ? AND address_type = ?`,
		p, stateSince, evalAt, transport, address, addrType)
	return err
}

// ListBTSightingTimes returns a device's observation timestamps (Unix ms),
// oldest first, since `since`. Presence sessions are built from these.
func (s *Store) ListBTSightingTimes(ctx context.Context, transport, address, addrType string, since int64,
	until int64) ([]int64, error) {
	q := `SELECT observed_at FROM bt_observations
		WHERE transport = ? AND address = ? AND address_type = ? AND observed_at >= ?`
	args := []any{transport, address, addrType, since}
	if until > 0 {
		q += ` AND observed_at <= ?`
		args = append(args, until)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY observed_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
