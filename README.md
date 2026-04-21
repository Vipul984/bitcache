# bitcache

A persistent L1 cache for Go services. Survives restarts.

> ⚠️ **Status:** Early development. This is a learning project building toward a real v0.1 release. Not production-ready yet.

## The problem bitcache solves

Every Go service that uses a local in-memory cache (`sync.Map`, `ristretto`, `bigcache`, `go-cache`) loses that cache on every restart. For services with large caches or expensive upstream calls, this causes:

- **Post-deploy latency spikes** — first requests hit the database/external APIs while L1 warms up
- **Post-deploy load spikes** — databases and paid external APIs get hammered right after every deploy
- **Kubernetes pod restart pain** — pods cycle frequently, each restart wipes L1
- **Slow CLI tools** — every invocation re-fetches data it just fetched seconds ago

bitcache is a Go library that works like `go-cache` or `ristretto` but persists to disk. Restart your service → cache is still warm. Built on the Bitcask storage model (append-only log + in-memory hash index) for microsecond reads and high write throughput.

## Design at a glance

- **Writes** are appended to a log file on disk. No seeks. No in-place updates.
- **Reads** do one in-memory index lookup plus one disk seek.
- **Restarts** rebuild the index by replaying the log (or reading hint files for fast startup).
- **Compaction** periodically rewrites old log files, keeping only live entries.
- **TTLs and eviction** layer on top of the Bitcask core to give it cache semantics.

## When to use bitcache

✅ Embed in a Go service where cache warmup after deploy hurts
✅ Cache expensive external API responses across restarts
✅ Cache computed values that are slow to recompute
✅ CLI tools that want a disk cache in `~/.yourapp/cache`
✅ Single-instance services or sticky-routed services

## When NOT to use bitcache

❌ Distributed rate limiting — use Redis
❌ Shared state between multiple service instances — use Redis
❌ Workloads with billions of keys (every key lives in RAM)
❌ Queues or pub/sub — use NATS, Kafka, or a real queue
❌ Multi-language services (Go-only library)

## Quickstart (planned for v0.1)

```go
import "github.com/YOUR_USERNAME/bitcache"

cache, err := bitcache.Open("/var/cache/myapp")
if err != nil { panic(err) }
defer cache.Close()

cache.Set([]byte("user:42"), []byte("Vipul"))
cache.SetWithTTL([]byte("token:abc"), []byte("..."), 5*time.Minute)

val, err := cache.Get([]byte("user:42"))
cache.Delete([]byte("user:42"))
```

## Roadmap

- **v0.1** — Core storage engine, basic API, TTLs, merge/compaction *(you are here)*
- **v0.2+** — Informed by real usage; likely: better eviction policies, benchmarks, more index backends
- **v1.0** — Production-hardened, API frozen, battle-tested

See [docs/ROADMAP.md](docs/ROADMAP.md) for detail.

## Project organization

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how bitcache works internally
- [docs/ROADMAP.md](docs/ROADMAP.md) — what ships when
- [docs/sessions/](docs/sessions/) — one document per weekend work session
- [docs/decisions/](docs/decisions/) — architecture decision records
- [CLAUDE.md](CLAUDE.md) — context for Claude Code when working on this repo

## License

MIT
