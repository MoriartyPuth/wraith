package transport

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestLineReaderFragmentedAndMixed(t *testing.T) {
	in := "I (32) boot: ESP-IDF v5\r\n{\"a\":1}\n" + strings.Repeat("x", 100) + "\n{\"b\":2}\r\n"
	// OneByteReader forces every read to return a single byte.
	lr := NewLineReader(iotest.OneByteReader(strings.NewReader(in)), 64)
	want := []struct {
		kind LineKind
		data string
	}{
		{LineLog, "I (32) boot: ESP-IDF v5"},
		{LineEvent, `{"a":1}`},
		{LineOversize, ""},
		{LineEvent, `{"b":2}`},
	}
	for i, w := range want {
		l, err := lr.Next()
		if err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if l.Kind != w.kind || string(l.Data) != w.data {
			t.Fatalf("line %d: got kind=%d %q, want kind=%d %q", i, l.Kind, l.Data, w.kind, w.data)
		}
	}
	if _, err := lr.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestLineReaderPartialAtEOF(t *testing.T) {
	lr := NewLineReader(strings.NewReader(`{"a":1}`+"\n"+`{"trunc`), 64)
	if _, err := lr.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := lr.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want ErrUnexpectedEOF, got %v", err)
	}
}

func TestLineReaderExactLimit(t *testing.T) {
	// 63 bytes + newline = 64 = limit: accepted. 64 + newline: oversize.
	ok := "{" + strings.Repeat("a", 62)
	over := "{" + strings.Repeat("a", 63)
	lr := NewLineReader(strings.NewReader(ok+"\n"+over+"\n"), 64)
	if l, _ := lr.Next(); l.Kind != LineEvent {
		t.Fatalf("63-byte line rejected: %v", l.Kind)
	}
	if l, _ := lr.Next(); l.Kind != LineOversize {
		t.Fatalf("64-byte line accepted: %v", l.Kind)
	}
}

func TestSeqTracker(t *testing.T) {
	st := NewSeqTracker(4)
	if v := st.Track("n", "aa-0", 0); v != (SeqVerdict{}) {
		t.Fatalf("first: %+v", v)
	}
	if v := st.Track("n", "aa-1", 1); v != (SeqVerdict{}) {
		t.Fatalf("in-order: %+v", v)
	}
	if v := st.Track("n", "aa-1", 1); !v.Duplicate {
		t.Fatal("duplicate not detected")
	}
	if v := st.Track("n", "aa-5", 5); v.Gap != 3 {
		t.Fatalf("gap = %d, want 3", v.Gap)
	}
	if v := st.Track("n", "bb-0", 0); !v.Restart || v.Gap != 0 {
		t.Fatalf("restart not detected: %+v", v)
	}
	if v := st.Track("other", "aa-0", 0); v.Duplicate {
		t.Fatal("dedup must be per sensor")
	}
	// LRU eviction: capacity 4, so aa-0 has been evicted for sensor n.
	st.Track("n", "bb-1", 1)
	st.Track("n", "bb-2", 2)
	if v := st.Track("n", "aa-0", 0); v.Duplicate {
		t.Fatal("evicted id still reported duplicate")
	}
}

func TestChecksum(t *testing.T) {
	js := []byte(`{"a":"tab\tescaped"}`)
	framed := AppendChecksum(js)
	if p, v := SplitChecksum(framed); v != ChecksumOK || string(p) != string(js) {
		t.Fatalf("valid: %v %q", v, p)
	}
	// Firmware's CRC of "123456789" is the standard check value cbf43926.
	if _, v := SplitChecksum([]byte("123456789\tcbf43926")); v != ChecksumOK {
		t.Fatal("check value mismatch with firmware CRC")
	}
	if _, v := SplitChecksum(js); v != ChecksumAbsent {
		t.Fatal("absent not detected")
	}
	// A duplicated USB packet spliced into a string keeps the JSON valid but
	// must fail the checksum.
	corrupt := []byte(`{"a":"tab\tescapedescaped"}` + string(framed[len(js):]))
	if _, v := SplitChecksum(corrupt); v != ChecksumMismatch {
		t.Fatal("corruption not detected")
	}
	if _, v := SplitChecksum([]byte("{}\tzzzzzzzz")); v != ChecksumMismatch {
		t.Fatal("garbage suffix accepted")
	}
}
