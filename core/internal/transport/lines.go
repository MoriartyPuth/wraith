// Package transport implements newline-delimited framing for sensor links.
// It never assumes one read equals one message.
package transport

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// LineKind classifies one framed line.
type LineKind int

const (
	// LineEvent looks like a protocol message (starts with '{').
	LineEvent LineKind = iota
	// LineLog is non-protocol output, e.g. ESP-IDF boot logs.
	LineLog
	// LineOversize was longer than the limit and has been discarded.
	LineOversize
)

// Line is one framed unit read from the link.
type Line struct {
	Kind LineKind
	Data []byte // copy; valid after the next Next() call. Empty for LineOversize.
	Size int    // original length in bytes (without terminator)
}

// LineReader splits a byte stream into lines bounded by maxLen.
type LineReader struct {
	r      *bufio.Reader
	maxLen int
}

// NewLineReader returns a reader that yields lines of at most maxLen bytes
// including the trailing newline. Longer lines are skipped up to the next
// newline and reported as LineOversize.
func NewLineReader(r io.Reader, maxLen int) *LineReader {
	return &LineReader{r: bufio.NewReaderSize(r, maxLen*2), maxLen: maxLen}
}

// Next returns the next line. It returns io.EOF when the stream ends; a
// trailing partial line without newline at EOF is dropped and reported as
// io.ErrUnexpectedEOF so the caller can count it.
func (lr *LineReader) Next() (Line, error) {
	var buf []byte
	size := 0
	oversize := false
	for {
		chunk, err := lr.r.ReadSlice('\n')
		size += len(chunk)
		if !oversize {
			if size > lr.maxLen {
				oversize, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && size > 0 {
			return Line{}, io.ErrUnexpectedEOF
		}
		return Line{}, err
	}
	if oversize {
		return Line{Kind: LineOversize, Size: size - 1}, nil
	}
	buf = bytes.TrimSuffix(buf, []byte("\n"))
	buf = bytes.TrimSuffix(buf, []byte("\r"))
	kind := LineLog
	if t := bytes.TrimLeft(buf, " \t"); len(t) > 0 && t[0] == '{' {
		kind, buf = LineEvent, t
	}
	return Line{Kind: kind, Data: buf, Size: len(buf)}, nil
}
