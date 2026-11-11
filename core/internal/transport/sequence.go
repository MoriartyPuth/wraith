package transport

import (
	"container/list"
	"strings"
	"sync"
)

// SeqVerdict is the result of tracking one event.
type SeqVerdict struct {
	Duplicate bool
	Restart   bool   // sensor boot_id changed
	Gap       uint64 // number of events missing before this one (same boot)
}

// SeqTracker detects duplicates (bounded LRU of event IDs per sensor),
// sensor restarts and sequence gaps. Nano event IDs are "<boot_id>-<seq>".
type SeqTracker struct {
	mu      sync.Mutex
	cap     int
	sensors map[string]*sensorSeq
}

type sensorSeq struct {
	bootID  string
	lastSeq uint64
	haveSeq bool
	seen    map[string]*list.Element
	order   *list.List
}

// NewSeqTracker remembers up to capacity recent event IDs per sensor.
func NewSeqTracker(capacity int) *SeqTracker {
	return &SeqTracker{cap: capacity, sensors: map[string]*sensorSeq{}}
}

// Track records an event and reports what it means for the stream.
func (t *SeqTracker) Track(sensorID, eventID string, seq uint64) SeqVerdict {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sensors[sensorID]
	if s == nil {
		s = &sensorSeq{seen: map[string]*list.Element{}, order: list.New()}
		t.sensors[sensorID] = s
	}
	if _, dup := s.seen[eventID]; dup {
		return SeqVerdict{Duplicate: true}
	}
	s.seen[eventID] = s.order.PushBack(eventID)
	if s.order.Len() > t.cap {
		old := s.order.Remove(s.order.Front()).(string)
		delete(s.seen, old)
	}

	var v SeqVerdict
	boot := bootIDOf(eventID)
	if s.haveSeq && boot != s.bootID {
		v.Restart = true
	} else if s.haveSeq && seq > s.lastSeq+1 {
		v.Gap = seq - s.lastSeq - 1
	}
	if !s.haveSeq || v.Restart || seq > s.lastSeq {
		s.lastSeq = seq
	}
	s.bootID, s.haveSeq = boot, true
	return v
}

func bootIDOf(eventID string) string {
	if i := strings.LastIndexByte(eventID, '-'); i > 0 {
		return eventID[:i]
	}
	return ""
}
