// Package record implements the on-disk record format: encode, decode, and CRC verification.
package record

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"time"
)

// HeaderSize is the fixed number of bytes before the variable-length key and value:
// 4 (CRC32) + 8 (timestamp) + 4 (key_len) + 4 (val_len) + 1 (flags) = 21
const HeaderSize = 21

var (
	// ErrCorruptRecord is returned when the CRC32 checksum does not match the record body.
	ErrCorruptRecord = errors.New("corrupt record: CRC mismatch")

	// ErrTruncated is returned when the buffer is too short to contain a valid record.
	ErrTruncated = errors.New("corrupt record: truncated")
)

// Record holds the decoded contents of a single on-disk record.
type Record struct {
	Timestamp int64
	Key       []byte
	Value     []byte
	Flags     byte
}

// Encode builds the full record bytes ready to append to a data file.
func Encode(key, value []byte, flags byte) []byte {
	size := HeaderSize + len(key) + len(value)
	buf := make([]byte, size)

	ts := time.Now().UnixNano()
	binary.BigEndian.PutUint64(buf[4:12], uint64(ts))
	binary.BigEndian.PutUint32(buf[12:16], uint32(len(key)))
	binary.BigEndian.PutUint32(buf[16:20], uint32(len(value)))

	copy(buf[20:], key)
	copy(buf[20+len(key):], value)
	buf[size-1] = flags

	checksum := crc32.ChecksumIEEE(buf[4:])
	binary.BigEndian.PutUint32(buf[0:4], checksum)

	return buf
}
