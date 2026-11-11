package storage

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/MoriartyPuth/wraith/core/migrations"
)

func TestMigrateIdempotentAndEmptyQueries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "w.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := fs.Glob(migrations.FS, "*.sql")
	v, err := s.SchemaVersion(ctx)
	if err != nil || v != len(files) {
		t.Fatalf("schema version = %d (want %d), %v", v, len(files), err)
	}
	s.Close()

	// Re-open: migrations must not re-apply.
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	sensors, total, err := s.ListSensors(ctx, Page{Limit: 10})
	if err != nil || total != 0 || len(sensors) != 0 {
		t.Fatalf("sensors: %v %d %v", sensors, total, err)
	}
	if _, err := s.GetSensor(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	st, err := s.Statistics(ctx)
	if err != nil || st.Events != 0 {
		t.Fatalf("stats: %+v %v", st, err)
	}
}

func TestListPagination(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, b := range []string{"AA:00:00:00:00:01", "AA:00:00:00:00:02", "AA:00:00:00:00:03"} {
		if _, err := s.db.Exec(`INSERT INTO access_points(bssid, first_seen, last_seen, last_channel) VALUES (?, ?, ?, 6)`, b, i, i); err != nil {
			t.Fatal(err)
		}
	}
	aps, total, err := s.ListAccessPoints(ctx, Page{Limit: 2, Offset: 0})
	if err != nil || total != 3 || len(aps) != 2 || aps[0].BSSID != "AA:00:00:00:00:03" {
		t.Fatalf("page1: %+v total=%d err=%v", aps, total, err)
	}
	aps, _, _ = s.ListAccessPoints(ctx, Page{Limit: 2, Offset: 2})
	if len(aps) != 1 || aps[0].BSSID != "AA:00:00:00:00:01" {
		t.Fatalf("page2: %+v", aps)
	}
	st, _ := s.Statistics(ctx)
	if st.APsByChannel[6] != 3 {
		t.Fatalf("aps_by_channel: %+v", st.APsByChannel)
	}
}
