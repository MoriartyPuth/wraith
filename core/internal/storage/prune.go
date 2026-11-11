package storage

import (
	"context"
	"database/sql"
	"time"
)

// PruneResult reports what a prune removed.
type PruneResult struct {
	Events       int64
	Observations int64
	Clients      int64 // client sightings whose last_seen fell before the cutoff
}

// pruneBatch bounds how many event rows are deleted per statement, so a large
// backlog is cleared over several short write locks instead of one long one.
const pruneBatch = 5000

// PruneEvents deletes events with received_at < cutoff (Unix ms) and the
// observations tied to them (ON DELETE CASCADE). Findings and the AP
// inventory are left untouched. It deletes in batches and stops when ctx is
// cancelled, returning what was removed so far.
func (s *Store) PruneEvents(ctx context.Context, cutoff int64) (PruneResult, error) {
	var res PruneResult
	for {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		var obsBefore, obsAfter int64
		err := s.WithTx(ctx, func(t *Tx) error {
			if err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations`).Scan(&obsBefore); err != nil {
				return err
			}
			r, err := t.tx.ExecContext(ctx,
				`DELETE FROM events WHERE id IN (SELECT id FROM events WHERE received_at < ? ORDER BY id LIMIT ?)`,
				cutoff, pruneBatch)
			if err != nil {
				return err
			}
			n, _ := r.RowsAffected()
			res.Events += n
			if err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations`).Scan(&obsAfter); err != nil {
				return err
			}
			res.Observations += obsBefore - obsAfter
			cr, err := t.tx.ExecContext(ctx, `DELETE FROM clients WHERE last_seen < ?`, cutoff)
			if err != nil {
				return err
			}
			if cn, _ := cr.RowsAffected(); cn > 0 {
				res.Clients += cn
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		// Fewer than a full batch means we reached the end.
		if res.Events == 0 || (res.Events%pruneBatch) != 0 {
			break
		}
	}
	return res, nil
}

// CountOlderThan returns how many events are older than cutoff (for logging).
func (s *Store) CountOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE received_at < ?`, cutoff).Scan(&n)
	return n, err
}

// RunPruner prunes every interval until ctx is cancelled. retention 0 means
// keep everything (the pruner then does nothing but still honours ctx). now
// is injectable for tests.
func (s *Store) RunPruner(ctx context.Context, retention, interval time.Duration, now func() time.Time, onPrune func(PruneResult)) {
	if retention <= 0 || interval <= 0 {
		return
	}
	// An initial prune clears any backlog from before this policy existed.
	prune := func() {
		cutoff := now().Add(-retention).UnixMilli()
		res, err := s.PruneEvents(ctx, cutoff)
		if (res.Events > 0 || res.Observations > 0 || res.Clients > 0) && onPrune != nil {
			onPrune(res)
		}
		_ = err // caller logs via onPrune; transient errors retry next tick
	}
	prune()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
		}
	}
}

var _ = sql.ErrNoRows
