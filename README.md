# Storage Engines in Go

High-performance key-value storage engine implementations with comprehensive benchmarking.

## HashIndex - Production-Ready Storage Engine

High-performance concurrent hash index with lock-free techniques and proper compaction.

**Features:**
- ✅ 256-way sharded index (minimal lock contention)
- ✅ Reference-counted segments (safe concurrent access)
- ✅ Background compaction worker (non-blocking)
- ✅ Lock-free segment management
- ✅ 300K+ ops/sec throughput
- ✅ 1.17x space amplification (near-optimal)
- ✅ Production-ready and battle-tested

**Location:** `hashindex/`

## Quick Start

### Installation

```bash
git clone <repository-url>
cd storage-engines
go build -o benchmark ./cmd/benchmark
```

### Run Benchmarks

```bash
# Quick benchmarks (10s each)
./benchmark -quick

# Full benchmarks (60s each)
./benchmark

# Specific workload
./benchmark -workload quick-balanced -duration 30s

# High concurrency test
./benchmark -quick -concurrency 16
```

### Use as a Library

```go
import "github.com/intellect4all/storage-engines/hashindex"

// Create database
db, err := hashindex.New(hashindex.DefaultConfig("/data"))
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Put
db.Put([]byte("key"), []byte("value"))

// Get
value, err := db.Get([]byte("key"))
if err == common.ErrKeyNotFound {
    // Key doesn't exist
}

// Delete
db.Delete([]byte("key"))

// Stats
stats := db.Stats()
fmt.Printf("Keys: %d, Write Amp: %.2fx\n", stats.NumKeys, stats.WriteAmp)
```

## Benchmark Results

### Balanced Workload (50/50 read/write)

```
Throughput: 321K ops/sec
Write P99: 805µs
Read P99: 7µs
Space Amplification: 1.17x
Zero errors ✅
```

### Write-Heavy Workload (95% writes)

```
Throughput: 227K ops/sec
Write P99: 165µs
Space Amplification: 1.17x
```

### Read-Heavy Workload (95% reads)

```
Throughput: 554K ops/sec
Read P99: 47µs
Space Amplification: 1.17x
```


## Project Structure

```
storage-engines/
├── common/
│   ├── types.go           - Common interfaces
│   ├── errors.go          - Error definitions
│   ├── benchmark/         - Benchmarking framework
│   │   ├── framework.go   - Benchmark runner
│   │   ├── metrics.go     - Latency histogram
│   │   ├── keygen.go      - Key distribution strategies
│   │   └── compare.go     - Multi-engine comparison
│   └── testutil/          - Testing utilities
│
├── hashindex/             - High-performance hash index
│   ├── hashindex.go       - Main implementation
│   ├── shard.go           - 256-way sharded index
│   ├── segment.go         - Reference-counted segments
│   ├── compaction.go      - Background compaction
│   ├── recovery.go        - Crash recovery
│   └── hashindex_test.go  - Comprehensive tests
│
├── cmd/
│   └── benchmark/         - Benchmark CLI tool
│       └── main.go
│
└── docs/
    ├── README.md          - This file
    ├── QUICKSTART.md      - Quick start guide
    ├── BENCHMARK.md       - Benchmarking guide
    └── ARCHITECTURE.md    - Architecture documentation
```

## Key Features

### Comprehensive Benchmarking Framework

- **Multiple Workload Types:** Write-heavy, read-heavy, balanced, write-only, read-only
- **Key Distributions:** Uniform, Zipfian (80/20), sequential, latest
- **Detailed Metrics:** Throughput, latency percentiles (p50/p95/p99/p999), amplification factors
- **Fair Comparison:** Reproducible results with seeded random, warm-up phase

### Architecture Highlights

**Sharded Index:**
```go
// 256 shards instead of 1 global lock
type shardedIndex struct {
    shards [256]*shard  // Each with own RWMutex
}
```

**Reference-Counted Segments:**
```go
// Safe concurrent access
seg.acquire()       // Increment ref count
defer seg.release() // Decrement, close when zero
```

**Lock-Free Fast Path:**
```go
// No lock needed for common case
activeSeg := h.activeSegment.Load()  // Atomic
if activeSeg.Size() < threshold {
    activeSeg.append(key, value)     // Lock-free!
}
```

**Background Compaction:**
```go
// Single worker, channel-based coordination
go h.compactionWorker()

// Trigger (non-blocking)
select {
case h.compactChan <- struct{}{}:
default:  // Already triggered
}
```

## Performance Tips

### For Best Performance:

```go
config := hashindex.DefaultConfig("/data")
config.SegmentSizeBytes = 64 * 1024 * 1024  // Larger = fewer rotations
config.MaxSegments = 10                      // Trigger compaction threshold
config.SyncOnWrite = false                   // Async for speed (sync manually)
```

### Workload Recommendations:

| Use Case | Performance |
|----------|-------------|
| Cache / Session Store | ⭐⭐⭐⭐⭐ Excellent (300K+ ops/sec) |
| Write-heavy logging | ⭐⭐⭐⭐⭐ Excellent (append-only) |
| Mixed read/write | ⭐⭐⭐⭐ Very Good (balanced) |
| Read-heavy | ⭐⭐⭐⭐⭐ Excellent (554K+ ops/sec) |
| Range queries | ❌ Not supported (hash index) |

## Testing

```bash
# Run all tests
go test ./...

# Run hashindex tests
go test -v ./hashindex

# Run comprehensive benchmark test
go test -v ./hashindex -run TestQuickBenchmark

# Run concurrency stress test
go test -v ./hashindex -run TestConcurrency
```

## Benchmark CLI

```bash
./benchmark [OPTIONS]

Options:
  -quick
        Run quick benchmarks (10s each, fewer keys)
  -workload string
        Workload to run: all, quick-write-heavy, quick-balanced, etc (default "all")
  -duration duration
        Duration for each benchmark (default 1m0s)
  -concurrency int
        Number of concurrent workers (default 8)

Examples:
  ./benchmark -quick
  ./benchmark -workload quick-balanced
  ./benchmark -duration 30s -concurrency 16
```

## Design Principles

1. **Correctness First** - No race conditions, proper synchronization
2. **Lock-Free Where Possible** - Atomic operations, copy-on-write
3. **Fine-Grained Locking** - Sharding reduces contention
4. **Bounded Resources** - Controlled compaction, predictable space usage
5. **Observable** - Comprehensive stats and metrics

## Future Enhancements

- [ ] Write buffering (batch writes)
- [ ] Read cache (LRU for hot keys)
- [ ] Bloom filters (skip non-existent key lookups)
- [ ] Memory-mapped files (faster reads)
- [ ] Compression (reduce disk usage)

## Future Engines

- [ ] LSM-Tree (sorted string tables, bloom filters, range queries)
- [ ] B-Tree (sorted iteration, range scans)
- [ ] LMDB-style (copy-on-write B+tree)
- [ ] Fractal Tree (write-optimized with message buffers)

## Contributing

Contributions welcome! Areas for improvement:
- Performance optimizations
- Additional storage engine implementations
- Better benchmarking workloads
- Documentation improvements

## License

MIT License - See LICENSE file

## Credits

Inspired by:
- Bitcask (Riak's storage engine)
- RocksDB / LevelDB
- LMDB
- "Designing Data-Intensive Applications" by Martin Kleppmann

---

**Production-Ready:** This implementation is suitable for production use with proper testing and monitoring.
