# bitcache Architecture

This document explains how bitcache works internally. If you understand the Bitcask paper, skim this. If not, read carefully — the design is deliberately simple but subtle.

## The core model

bitcache stores data in two places that mirror each other:

1. **On disk:** an append-only log of records. New writes go to the end. Nothing is ever overwritten in place.
2. **In memory:** a hash map (the "keydir") from key → exact disk location of the most recent record for that key.

Every read is: one map lookup + one disk seek. Every write is: one disk append + one map update.

## The disk layout

Data lives in files named by a monotonically increasing integer:

```
/var/cache/myapp/
├── 00000001.data          ← immutable, sealed
├── 00000002.data          ← immutable, sealed
├── 00000003.data          ← the "active" file, currently being appended to
├── 00000001.hint          ← optional, generated during merge
└── 00000002.hint
```

Only one file is **active** at a time — the newest one. All writes go to it. When it crosses a configured size threshold (default 1 GB), bitcache closes it, marks it immutable, and opens the next number as the new active file.

## The record format

Every record on disk is a variable-length binary blob:

```
┌────────┬────────┬──────────┬──────────┬──────────┬──────────┬──────────┐
│  CRC32 │  TS    │ key_len  │ val_len  │   key    │  value   │  flags   │
│ 4 byte │ 8 byte │  4 byte  │  4 byte  │ var      │  var     │ 1 byte   │
└────────┴────────┴──────────┴──────────┴──────────┴──────────┴──────────┘
```

- **CRC32** — checksum over the rest of the record; detects corruption
- **Timestamp** — unix nanoseconds, used for TTL and tie-breaking during recovery
- **Key length, Value length** — sizes of the upcoming key and value bytes
- **Key, Value** — raw bytes
- **Flags** — bit flags: bit 0 = tombstone (deletion marker), bit 1 = has_ttl, etc.

Deletes are just records with the tombstone flag set. Same format, different flag.

## The in-memory keydir

A `map[string]KeydirEntry` where each entry is:

```go
type KeydirEntry struct {
    FileID    uint32  // which data file
    Offset    int64   // byte offset within the file
    Size      uint32  // how many bytes to read
    Timestamp int64   // for TTL checks
}
```

That's all we keep in memory per key. The values themselves live on disk. The tradeoff: **every key must fit in RAM** (around 50-80 bytes per entry), but values can be huge without affecting memory.

For 1 million keys, the keydir is roughly 50-80 MB of RAM. For 10 million, 500-800 MB. At 100 million keys, you need BadgerDB or RocksDB instead — bitcache is not the right tool.

## The write path

`Set(key, value)`:

1. Build the record bytes (CRC, timestamp, lengths, key, value, flags)
2. Append to the active file's writer
3. Optionally `fsync` (depends on sync policy — per-write vs periodic)
4. Compute the new keydir entry: `{FileID, Offset, Size, Timestamp}`
5. Atomically update `keydir[key] = newEntry`
6. If the active file now exceeds max size, rotate (close it, open a new one)

## The read path

`Get(key)`:

1. Look up `key` in the keydir → get `KeydirEntry` (or miss → return ErrNotFound)
2. Check TTL — if expired, remove from keydir and return ErrNotFound
3. Open (or reuse cached fd for) the file `FileID`
4. `pread` at `Offset` for `Size` bytes
5. Decode the record, verify CRC, return the value

If the CRC fails, the file is corrupt — return an error and log. Don't silently return garbage.

## The delete path

`Delete(key)`:

1. If key not in keydir, return (nothing to do, but also not an error)
2. Write a tombstone record to the active file
3. Remove the key from the keydir

The tombstone stays on disk until the next merge collects it. It's needed so that if we crash and replay, we re-learn that the key was deleted rather than re-reading the old value.

## Crash recovery

When bitcache opens a directory, it rebuilds the keydir from scratch:

1. List all `.data` files, sorted by FileID ascending (oldest first)
2. For each file:
   - If a matching `.hint` file exists, read it (much faster) and populate the keydir
   - Otherwise, scan the full data file, decoding each record in order
3. For each record encountered:
   - If tombstone → `delete(keydir, key)`
   - Otherwise → `keydir[key] = entry` (overwrites any earlier entry for this key)

Because we process oldest-to-newest, the final keydir correctly points only to the newest live record for each key. Stale entries in old files are invisible because nothing points to them.

## Merge / compaction

Over time, old data files fill up with stale records (overwritten keys, deleted keys). Wasted space. Merge reclaims it:

1. Pick a set of immutable files to merge (typically all or all older than a threshold)
2. Walk them in order, for each record:
   - Look up the key in the current keydir
   - If the keydir entry points to *this* record → it's live, copy it to a new merged file
   - Otherwise → skip it (it's stale)
3. Atomically swap: update keydir to point to new merged files, delete old files
4. Optionally write hint files alongside merged data files for fast future startups

Key correctness requirements:
- **Merge never blocks writes.** The active file keeps accepting writes during merge. Only immutable files are touched.
- **Merge output is written to temp names, then atomically renamed** so a crash mid-merge never leaves the directory in a bad state.
- **Keydir updates during merge** use compare-and-swap semantics: only update a keydir entry if it's still pointing to the old location we expected. Otherwise, a concurrent write has moved it elsewhere and we leave it alone.

## Hint files

A `.hint` file is a compact index written alongside a sealed data file. It contains one row per live key: `(key, FileID, Offset, Size, Timestamp)`. On startup, if a hint file exists, we read it and populate the keydir without touching the data file at all.

This makes startup O(number of live keys) instead of O(total bytes on disk). For a 10 GB data directory with 1 million live keys, startup goes from ~30 seconds to ~0.5 seconds.

## The cache layer (on top of Bitcask)

Plain Bitcask is a durable KV store. To make it a cache, bitcache adds:

- **TTLs.** Stored in the record's timestamp + flags. On read, we check expiry and lazily evict. A background goroutine periodically sweeps for expired entries.
- **Size-based eviction.** An LRU tracker in memory maintains recency per key. When cache size exceeds the configured limit, the LRU pops evict victims and we tombstone them.
- **Stats.** Hit rate, miss rate, byte size, entry count, exposed via a stats method.

The cache layer is a thin wrapper around the Bitcask core. If someone wants just the storage engine without cache semantics, they can use the internal package directly.

## Concurrency model

- **Reads** are lock-free against the keydir via `sync.Map` or an RWMutex on a plain map. Multiple concurrent readers, no blocking.
- **Writes** are serialized through a single writer goroutine that owns the active file. Public `Set`/`Delete` methods send the write request over a channel. This avoids contention on the active file's write offset.
- **Merge** runs in its own goroutine. It only reads immutable files and uses atomic rename for output. Writes are never blocked.
- **fsync policy** is configurable: per-write (slowest, safest), per-batch (grouped), or periodic (fastest, small data-loss window on crash).

## Known tradeoffs and limits

- **RAM scales with keys, not values.** Great for many small keys with large values. Bad for billions of tiny key-value pairs.
- **No range queries.** The keydir is a hash map. If you need sorted iteration, use a B-tree-based store like BoltDB.
- **No secondary indexes.** Single key → value only.
- **Write amplification from merge.** Merging rewrites data. Expected cost of reclaiming space.
- **Not distributed.** One process, one directory. Replication is out of scope for v0.1.

## References

- [Bitcask paper (Basho)](https://riak.com/assets/bitcask-intro.pdf) — the original 6-page design document
- [prologic/bitcask](https://git.mills.io/prologic/bitcask) — a more full-featured Go implementation, good for reference
- [SarthakMakhija/bitcask](https://github.com/SarthakMakhija/bitcask) — educational Go implementation with clear code comments
