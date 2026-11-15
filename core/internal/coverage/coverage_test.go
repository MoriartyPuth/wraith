package coverage

import (
	"reflect"
	"testing"
)

func seg(from, to int64, kind string, classic bool) Segment {
	return Segment{From: from, To: to, Kind: kind, Classic: classic}
}

func TestBuildModesGapsAndMerge(t *testing.T) {
	changes := []Change{
		{At: 300, Mode: "bluetooth"},                // switch
		{At: 200, Mode: "wifi"},                     // unsorted input; same as initial: merges
		{At: 400, Mode: "bluetooth", Classic: true}, // Classic on in place
		{At: 450, Mode: "bluetooth", Classic: true}, // periodic repeat: merges
		{At: 600, Mode: "wifi", Classic: true},      // Classic is meaningless in Wi-Fi mode
		{At: 2000, Mode: "bluetooth"},               // after the window: ignored
	}
	gaps := []Gap{{From: 700, To: 800}, {From: -50, To: 120}, {From: 950, To: 5000}}
	got := Build(100, 1000, Change{Mode: "wifi"}, changes, gaps)
	want := []Segment{
		seg(100, 120, KindNoData, false), // gap starting before the window
		seg(120, 300, KindWifi, false),
		seg(300, 400, KindBluetooth, false),
		seg(400, 600, KindBluetooth, true),
		seg(600, 700, KindWifi, false),
		seg(700, 800, KindNoData, false),
		seg(800, 950, KindWifi, false),
		seg(950, 1000, KindNoData, false), // gap running past the end
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	// The caller's slice is not reordered.
	if changes[0].At != 300 || changes[1].At != 200 {
		t.Fatal("Build reordered its input")
	}
	tot := Totals(got)
	if tot[KindWifi]+tot[KindBluetooth]+tot[KindNoData] != 900 || tot["classic"] != 200 || tot[KindBluetooth] != 300 {
		t.Fatalf("totals %v", tot)
	}
}

func TestBuildEdges(t *testing.T) {
	if Build(10, 10, Change{}, nil, nil) != nil || Build(10, 5, Change{}, nil, nil) != nil {
		t.Fatal("empty window must give no segments")
	}
	// Initial Bluetooth with Classic carries into the window.
	got := Build(0, 100, Change{Mode: "bluetooth", Classic: true}, nil, nil)
	if !reflect.DeepEqual(got, []Segment{seg(0, 100, KindBluetooth, true)}) {
		t.Fatalf("initial: %+v", got)
	}
	// A change exactly at start applies from the start.
	got = Build(0, 100, Change{Mode: "wifi"}, []Change{{At: 0, Mode: "bluetooth"}}, nil)
	if !reflect.DeepEqual(got, []Segment{seg(0, 100, KindBluetooth, false)}) {
		t.Fatalf("change at start: %+v", got)
	}
	// Whole window without heartbeats; unknown mode strings count as Wi-Fi.
	got = Build(0, 100, Change{Mode: "???"}, nil, []Gap{{From: 0, To: 100}})
	if !reflect.DeepEqual(got, []Segment{seg(0, 100, KindNoData, false)}) {
		t.Fatalf("no data: %+v", got)
	}
	got = Build(0, 100, Change{Mode: "???"}, nil, nil)
	if got[0].Kind != KindWifi {
		t.Fatalf("unknown mode: %+v", got)
	}
	// A mode change inside a gap takes effect when the gap ends.
	got = Build(0, 100, Change{Mode: "wifi"}, []Change{{At: 50, Mode: "bluetooth"}}, []Gap{{From: 40, To: 60}})
	want := []Segment{seg(0, 40, KindWifi, false), seg(40, 60, KindNoData, false), seg(60, 100, KindBluetooth, false)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("change in gap: %+v", got)
	}
	// Segments always tile the window exactly.
	for _, s := range [][]Segment{got} {
		for i := 1; i < len(s); i++ {
			if s[i].From != s[i-1].To {
				t.Fatal("segments do not tile")
			}
		}
	}
}
