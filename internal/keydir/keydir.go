// Package keydir implements the in-memory hash index mapping keys to their on-disk locations.
package keydir

import "sync"

// Entry holds the disk location of the most recent record for a key.
type Entry struct {
	FileID    uint32
	Offset    int64
	Size      uint32
	Timestamp int64
}

// KeyDir is a concurrency-safe in-memory index from key to disk location.
type KeyDir struct {
	mu sync.RWMutex
	m  map[string]Entry
}
