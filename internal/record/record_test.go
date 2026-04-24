package record

import (
	"bytes"
	"testing"
)

func TestDecodeTruncated(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{name: "empty buffer", buf: []byte{}},
		{name: "one byte", buf: []byte{0x01}},
		{name: "header incomplete", buf: make([]byte, HeaderSize-1)},
		{name: "body missing", buf: Encode([]byte("key"), []byte("value"), 0)[:HeaderSize]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.buf)
			if err != ErrTruncated {
				t.Fatalf("got %v, want ErrTruncated", err)
			}
		})
	}
}

func TestDecodeCorruptCRC(t *testing.T) {
	buf := Encode([]byte("key"), []byte("value"), 0)

	buf[5] ^= 0xFF // flip all bits in one byte of the body

	_, err := Decode(buf)
	if err != ErrCorruptRecord {
		t.Fatalf("got %v, want ErrCorruptRecord", err)
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		key   []byte
		value []byte
		flags byte
	}{
		{name: "normal", key: []byte("name"), value: []byte("vipul"), flags: 0},
		{name: "empty value", key: []byte("key"), value: []byte{}, flags: 0},
		{name: "empty key", key: []byte{}, value: []byte("value"), flags: 0},
		{name: "both empty", key: []byte{}, value: []byte{}, flags: 0},
		{name: "tombstone flag", key: []byte("dead"), value: []byte{}, flags: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := Encode(tt.key, tt.value, tt.flags)

			got, err := Decode(buf)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if !bytes.Equal(got.Key, tt.key) {
				t.Errorf("key: got %q, want %q", got.Key, tt.key)
			}
			if !bytes.Equal(got.Value, tt.value) {
				t.Errorf("value: got %q, want %q", got.Value, tt.value)
			}
			if got.Flags != tt.flags {
				t.Errorf("flags: got %d, want %d", got.Flags, tt.flags)
			}
			if got.Timestamp == 0 {
				t.Error("timestamp is zero")
			}
		})
	}
}
