package storage

import (
	"context"
	"testing"
	"time"
)

// base is a fixed reference so tests don't race the wall clock at bucket edges.
var base = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func seedEvents(t *testing.T, s *Store, ageDays []int) {
	t.Helper()
	ctx := context.Background()
	for i, d := range ageDays {
		at := base.AddDate(0, 0, -d).UnixMilli()
		err := s.WithTx(ctx, func(tx *Tx) error {
			row, _, err := tx.InsertEvent(ctx, StoredEvent{
				EventID: id(i), SensorID: "nano-1", Source: "wraith-nano", EventType: "wifi.beacon_observed",
				SensorTS: 0, ReceivedAt: at, Payload: []byte(`{}`),
			})
			if err != nil {
				return err
			}
			b := "AA:BB:CC:00:00:01"
			ch, rssi := 6, -50
			return tx.InsertObservation(ctx, row, b, "nano-1", &ch, &rssi, at)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func id(i int) string { return "e-" + time.Now().Format("150405") + "-" + itoa(i) }
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestPruneByAge(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// 10 events: ages 0,1,2,...,9 days. Retain 5 days → ages 6..9 pruned (4).
	seedEvents(t, s, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	cutoff := base.Add(-time.Duration(5.5*24) * time.Hour).UnixMilli() // between the 5- and 6-day buckets

	n, _ := s.CountOlderThan(ctx, cutoff)
	if n != 4 {
		t.Fatalf("older count = %d, want 4", n)
	}
	res, err := s.PruneEvents(ctx, cutoff)
	if err != nil || res.Events != 4 || res.Observations != 4 {
		t.Fatalf("prune: %+v %v", res, err)
	}
	_, total, _ := s.ListEvents(ctx, Page{Limit: 100}, EventFilter{})
	if total != 6 {
		t.Fatalf("remaining events = %d, want 6", total)
	}
	var obs int
	s.db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&obs)
	if obs != 6 {
		t.Fatalf("remaining observations = %d, want 6", obs)
	}
	// Idempotent: second prune removes nothing.
	res, _ = s.PruneEvents(ctx, cutoff)
	if res.Events != 0 {
		t.Fatalf("second prune removed %d", res.Events)
	}
}

func TestPruneKeepsFindings(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	old := base.AddDate(0, 0, -30).UnixMilli()
	s.WithTx(ctx, func(tx *Tx) error {
		return tx.InsertFinding(ctx, Finding{ID: "f-1", Category: "deauth_activity", Severity: "medium",
			Confidence: 0.5, Classification: "suspicious", Explanation: "e", Evidence: []string{"e-x"}, CreatedAt: old})
	})
	seedEvents(t, s, []int{30})
	if _, err := s.PruneEvents(ctx, base.AddDate(0, 0, -1).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	_, total, _ := s.ListFindings(ctx, Page{Limit: 10})
	if total != 1 {
		t.Fatalf("findings pruned: %d", total)
	}
}

func TestPrunerLoopInitialSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	seedEvents(t, s, []int{0, 10})
	done := make(chan PruneResult, 1)
	go s.RunPruner(ctx, 5*24*time.Hour, time.Hour, func() time.Time { return base }, func(r PruneResult) {
		select {
		case done <- r:
		default:
		}
	})
	select {
	case r := <-done:
		if r.Events != 1 {
			t.Fatalf("initial sweep removed %d, want 1", r.Events)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pruner did not sweep")
	}
	cancel()
}

func TestPrunerDisabled(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()
	seedEvents(t, s, []int{100})
	// retention 0: returns immediately, deletes nothing.
	s.RunPruner(ctx, 0, time.Hour, time.Now, func(PruneResult) { t.Fatal("pruned with retention 0") })
	_, total, _ := s.ListEvents(ctx, Page{Limit: 10}, EventFilter{})
	if total != 1 {
		t.Fatalf("events = %d", total)
	}
}
