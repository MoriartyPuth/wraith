package transport

import (
	"bytes"
	"encoding/hex"
	"hash/crc32"
)

// Checksum verdicts for one framed line.
type Checksum int

const (
	ChecksumAbsent   Checksum = iota // no "\t<8 hex>" suffix
	ChecksumOK                       // suffix present and matches
	ChecksumMismatch                 // suffix present, CRC wrong (corrupted line)
)

// SplitChecksum separates "<json>\t<crc32 hex>" (see docs/protocol.md,
// Framing). It returns the JSON part and the verdict. A raw tab cannot occur
// inside JSON, so the last tab is the separator.
func SplitChecksum(line []byte) ([]byte, Checksum) {
	i := bytes.LastIndexByte(line, '\t')
	if i < 0 || len(line)-i-1 != 8 {
		return line, ChecksumAbsent
	}
	var want [4]byte
	if _, err := hex.Decode(want[:], line[i+1:]); err != nil {
		return line, ChecksumMismatch // looks like a suffix but is garbage
	}
	payload := line[:i]
	got := crc32.ChecksumIEEE(payload)
	if got == uint32(want[0])<<24|uint32(want[1])<<16|uint32(want[2])<<8|uint32(want[3]) {
		return payload, ChecksumOK
	}
	return payload, ChecksumMismatch
}

// AppendChecksum frames payload as "<payload>\t<crc32 hex>" (tests, tools).
func AppendChecksum(payload []byte) []byte {
	c := crc32.ChecksumIEEE(payload)
	out := append(append([]byte{}, payload...), '\t')
	return append(out, []byte(hex.EncodeToString([]byte{byte(c >> 24), byte(c >> 16), byte(c >> 8), byte(c)}))...)
}
