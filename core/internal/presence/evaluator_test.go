package presence

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

func TestEvaluateTransitions(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.UnixMilli(1_000_000_000)
	ev := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ev.now = func() time.Time { return now }

	const addr = "AA:BB:CC:DD:EE:FF"
	putDevice := func(lastSeen int64) {
		t.Helper()
		err := st.WithTx(ctx, func(tx *storage.Tx) error {
			return tx.PutBTDevice(ctx, storage.BTDevice{
				Transport: storage.BTBLE, Address: addr, AddressType: "public",
				FirstSeen: now.UnixMilli() - 3_600_000, LastSeen: lastSeen,
				Services: []string{},
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Device is currently present.
	putDevice(now.UnixMilli() - 1000)

	// Add the watch while away, so the first evaluation sees an arrival.
	if err := st.PutWatch(ctx, storage.WatchEntry{
		Transport: storage.BTBLE, Address: addr, AddressType: "public",
		AddedAt: now.UnixMilli() - 2000, Present: false,
	}); err != nil {
		t.Fatal(err)
	}

	fs, err := ev.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Category != "bt_watch_arrived" || fs[0].Severity != "info" {
		t.Fatalf("arrival findings: %+v", fs)
	}
	if fs[0].BTDevice == nil || *fs[0].BTDevice != "ble:public:"+addr {
		t.Fatalf("finding device: %+v", fs[0].BTDevice)
	}
	w, _ := st.GetWatch(ctx, storage.BTBLE, addr, "public")
	if !w.Present {
		t.Fatalf("watch not marked present: %+v", w)
	}

	// Same state: no new finding.
	if fs, _ = ev.Evaluate(ctx); len(fs) != 0 {
		t.Fatalf("repeat arrival: %+v", fs)
	}

	// Device goes silent for > GapMS → departure.
	putDevice(now.UnixMilli() - GapMS - 1000)
	fs, err = ev.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Category != "bt_watch_left" || fs[0].Severity != "low" {
		t.Fatalf("departure findings: %+v", fs)
	}
	if w, _ = st.GetWatch(ctx, storage.BTBLE, addr, "public"); w.Present {
		t.Fatalf("watch still present: %+v", w)
	}
	if fs, _ = ev.Evaluate(ctx); len(fs) != 0 {
		t.Fatalf("repeat departure: %+v", fs)
	}

	// The label drives the explanation when set.
	putDevice(now.UnixMilli() - 500)
	if err := st.PutWatch(ctx, storage.WatchEntry{
		Transport: storage.BTBLE, Address: addr, AddressType: "public",
		Label: "My tag", AddedAt: now.UnixMilli(), Present: false,
	}); err != nil {
		t.Fatal(err)
	}
	fs, _ = ev.Evaluate(ctx)
	if len(fs) != 1 || fs[0].Category != "bt_watch_arrived" {
		t.Fatalf("labelled arrival: %+v", fs)
	}
	if got := fs[0].Explanation; got != `Watched device "My tag" was heard again.` {
		t.Fatalf("explanation: %q", got)
	}
}

func TestEvaluateIgnoresUnwatched(t *testing.T) {
	ctx := context.Background()
	st, err := storage.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ev := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if fs, err := ev.Evaluate(ctx); err != nil || len(fs) != 0 {
		t.Fatalf("empty watchlist: %v %v", fs, err)
	}
}
