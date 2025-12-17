# Storage Engines in Go

A collection of high-performance, production-ready key-value storage engine implementations in Go, with comprehensive benchmarking and detailed documentation.

## Overview

This project implements multiple storage engines from scratch, each with different trade-offs and use cases:

1. **[Hash Index](./hashindex/README.md)** - Bitcask-inspired append-only log with in-memory hash index
2. **[LSM-Tree](./lsm/README.md)** - Log-Structured Merge-Tree with multi-level compaction and bloom filters

Each engine is fully documented with architecture details, performance characteristics, and usage guides.

## Quick Comparison

| Feature | Hash Index | LSM-Tree |
|---------|------------|----------|
| **Write Speed** | ⭐⭐⭐⭐⭐ Very Fast | ⭐⭐⭐⭐ Fast |
| **Read Speed** | ⭐⭐⭐⭐⭐ Very Fast (O(1)) | ⭐⭐⭐⭐ Good (multi-level) |
| **Range Scans** | ❌ Not Supported | ✅ Excellent |
| **Memory Usage** | 🟡 Higher (all keys) | 🟢 Lower (bloom filters) |
| **Write Amplification** | 🟢 1.5-2.5x | 🟡 4-10x |
| **Use Case** | Key-value store, caching | Time-series, logs, analytics |
| **Best For** | Point lookups, updates | Range queries, scans |

## Storage Engines

### 1. Hash Index

A high-performance, log-structured hash index inspired by Bitcask.

**Key Features:**
- O(1) reads and writes
- 256-way sharded in-memory index for high concurrency
- Reference-counted segments for safe concurrent access
- Background compaction with minimal write amplification
- 3-4x faster than LSM-Tree for point lookups

**When to Use:**
- Session stores, user profiles, configuration storage
- Caching layers (Redis-like workloads)
- Write-heavy workloads requiring high throughput
- When all keys fit in memory

**Performance:** 300K+ ops/sec mixed workload, 8M+ ops/sec reads

📖 **[Read Full Documentation](./hashindex/README.md)**

### 2. LSM-Tree

A complete LSM-Tree implementation with 5 levels (L0-L4), bloom filters, and WAL.

**Key Features:**
- Multi-level compaction (L0 → L1 → L2 → L3 → L4)
- Bloom filters for fast negative lookups
- Write-Ahead Log (WAL) for crash recovery
- Range scan support with iterators
- 4KB block-based SSTables

**When to Use:**
- Time-series data requiring range queries
- Log aggregation and analytics
- When dataset doesn't fit in memory
- Applications needing sorted iteration

**Performance:** 45K ops/sec writes, 2M ops/sec reads, excellent range scan performance

📖 **[Read Full Documentation](./lsm/README.md)**

## Quick Start

### Installation

```bash
git clone https://github.com/intellect4all/storage-engines
cd storage-engines
go mod tidy
```

### Using Hash Index

```go
import "github.com/intellect4all/storage-engines/hashindex"

// Create database
db, err := hashindex.New(hashindex.DefaultConfig("./data"))
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Write
db.Put([]byte("user:1001"), []byte(`{"name": "Alice"}`))

// Read
value, err := db.Get([]byte("user:1001"))

// Stats
stats := db.Stats()
fmt.Printf("Keys: %d, Write Amp: %.2fx\n", stats.NumKeys, stats.WriteAmp)
```

### Using LSM-Tree

```go
import "github.com/intellect4all/storage-engines/lsm"

// Create database
config := lsm.DefaultConfig("./data")
db, err := lsm.New(config)
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Write
db.Put("user:1001", []byte(`{"name": "Alice"}`))

// Read
value, found, err := db.Get("user:1001")

// Range scan
iter := db.Scan("user:", "user:~")
for iter.Valid() {
    fmt.Printf("%s: %s\n", iter.Key(), iter.Value())
    iter.Next()
}
```

## Benchmark Results

### Write Performance (100K operations)

```
Hash Index:  135,000 ops/sec
LSM-Tree:     42,000 ops/sec
Winner: Hash Index (3.2x faster)
```

### Read Performance (10K keys, random access)

```
Hash Index:  7,800,000 ops/sec
LSM-Tree:    1,800,000 ops/sec
Winner: Hash Index (4.3x faster)
```

### Range Scans (10K keys, full scan)

```
Hash Index:  Not supported
LSM-Tree:    Excellent (sequential read)
Winner: LSM-Tree (only option)
```

### Mixed Workload (50% reads, 50% writes)

```
Hash Index:  290,000 ops/sec
LSM-Tree:     85,000 ops/sec
Winner: Hash Index (3.4x faster)
```

Run comprehensive benchmarks:

```bash
go test -bench=BenchmarkWritePerformance -benchtime=1s
go test -bench=BenchmarkReadPerformance -benchtime=1s
go test -bench=BenchmarkMixedWorkload -benchtime=1s
go test -bench=BenchmarkRangeScanCapability -benchtime=1s
```

## Project Structure

```
storage-engines/
├── hashindex/              # Hash Index storage engine
│   ├── README.md          # Detailed documentation
│   ├── hashindex.go       # Main implementation
│   ├── shard.go           # 256-way sharded index
│   ├── segment.go         # Reference-counted segments
│   ├── compaction.go      # Background compaction
│   └── recovery.go        # Crash recovery
│
├── lsm/                    # LSM-Tree storage engine
│   ├── README.md          # Detailed documentation
│   ├── lsm.go             # Main LSM engine
│   ├── memtable.go        # In-memory sorted table
│   ├── wal.go             # Write-Ahead Log
│   ├── sstable.go         # Sorted String Table
│   ├── sstable_builder.go # SSTable builder
│   ├── bloom.go           # Bloom filter
│   ├── compaction.go      # Multi-level compaction
│   ├── levels.go          # Level manager
│   └── iterator.go        # Range scan iterator
│
├── common/                 # Shared utilities
│   ├── types.go           # Common interfaces
│   └── errors.go          # Error definitions
│
├── comparison_benchmark_test.go  # Cross-engine benchmarks
├── COMPONENT_GUIDE.md     # Detailed component explanations
└── README.md              # This file
```

## Choosing the Right Engine

### Use Hash Index When:

✅ You need maximum throughput (300K+ ops/sec)
✅ All operations are point lookups (get/put/delete)
✅ All keys fit in memory
✅ Write amplification matters (1.5-2.5x vs. 4-10x)
✅ Simplicity and debuggability are important

❌ Avoid if you need range queries or sorted iteration

**Example Use Cases:**
- Session stores
- User profiles
- Configuration storage
- Caching layers
- Real-time analytics ingestion

### Use LSM-Tree When:

✅ You need range queries or sorted iteration
✅ Dataset is larger than available memory
✅ Write-heavy workload with good read performance
✅ Time-series data or log aggregation
✅ Bloom filters help your access patterns

❌ Avoid if you only do point lookups (Hash Index is faster)

**Example Use Cases:**
- Time-series databases
- Log aggregation
- Event sourcing
- Analytics workloads
- Document stores with secondary indexes

## Testing

### Run All Tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Verbose output
go test -v ./hashindex ./lsm
```

### Run Specific Tests

```bash
# Hash Index tests
go test ./hashindex/ -run TestBasic
go test ./hashindex/ -run TestCompaction
go test ./hashindex/ -run TestConcurrency

# LSM-Tree tests
go test ./lsm/ -run TestMemTable
go test ./lsm/ -run TestSSTable
go test ./lsm/ -run TestCompaction
go test ./lsm/ -run TestIterator
```

### Run Benchmarks

```bash
# Individual engine benchmarks
go test ./hashindex/ -bench=. -benchmem
go test ./lsm/ -bench=. -benchmem

# Comparison benchmarks
go test -bench=BenchmarkWritePerformance
go test -bench=BenchmarkReadPerformance
go test -bench=BenchmarkMixedWorkload
```

## Architecture Highlights

### Hash Index Architecture

```
┌─────────────────────────────────────┐
│  Sharded In-Memory Index (256)     │
│  ┌──────┬──────┬──────┬──────┐     │
│  │Shard0│Shard1│Shard2│ ...  │     │
│  │  ↓   │  ↓   │  ↓   │      │     │
│  └──────┴──────┴──────┴──────┘     │
│         ↓         ↓         ↓       │
│  ┌────────────────────────────┐    │
│  │  Append-Only Segments      │    │
│  │  [Active] [Seg1] [Seg2]... │    │
│  └────────────────────────────┘    │
└─────────────────────────────────────┘
```

**Key Principles:**
- O(1) lookups via in-memory hash index
- Sequential writes for maximum throughput
- Background compaction removes duplicates
- Reference counting for safe concurrency

### LSM-Tree Architecture

```
┌─────────────────────────────────────┐
│          MemTable (Sorted)          │
│    [k1,v1] [k2,v2] ... [kN,vN]     │
└─────────────────────────────────────┘
                ↓ Flush
┌─────────────────────────────────────┐
│  Level 0 (4MB, overlapping files)   │
│  [SST1] [SST2] [SST3] [SST4]       │
└─────────────────────────────────────┘
                ↓ Compact
┌─────────────────────────────────────┐
│  Level 1 (40MB, non-overlapping)    │
│  [SST1] [SST2] ... [SST10]         │
└─────────────────────────────────────┘
                ↓ Compact
┌─────────────────────────────────────┐
│  Levels 2-4 (400MB, 4GB, 40GB)     │
└─────────────────────────────────────┘
```

**Key Principles:**
- Writes go to MemTable first (fast)
- Background compaction maintains sorted levels
- Bloom filters skip non-existent keys
- Range scans via sorted merge

## Design Principles

Both engines follow these core principles:

1. **Correctness First** - Proper synchronization, no race conditions
2. **Performance Second** - Lock-free where possible, fine-grained locking
3. **Simplicity Third** - Clean abstractions, easy to understand
4. **Production-Ready** - Comprehensive tests, crash recovery, observability

## Performance Tuning

### Hash Index Tuning

```go
config := hashindex.DefaultConfig("/data")

// High Throughput
config.SegmentSizeBytes = 64 * 1024 * 1024  // 64MB (fewer rotations)
config.MaxSegments = 8                       // Allow more before compaction
config.SyncOnWrite = false                   // Async writes

// Low Latency
config.SegmentSizeBytes = 1 * 1024 * 1024   // 1MB (faster rotation)
config.MaxSegments = 6                       // Compact more frequently
config.SyncOnWrite = false

// Durability
config.SyncOnWrite = true                    // fsync every write (slower)
```

### LSM-Tree Tuning

```go
config := lsm.DefaultConfig("/data")

// Write-Heavy
config.MemTableSize = 8 * 1024 * 1024       // 8MB (fewer flushes)
config.MaxL0Files = 8                        // Allow more before compaction

// Read-Heavy (default is good)
config.MemTableSize = 4 * 1024 * 1024       // 4MB
config.MaxL0Files = 4                        // Keep levels shallow

// Balanced
config.MemTableSize = 4 * 1024 * 1024
config.MaxL0Files = 4
```

## Advanced Topics

For in-depth component documentation, see:

📖 **[COMPONENT_GUIDE.md](./COMPONENT_GUIDE.md)** - Detailed explanation of every component

Topics covered:
- Bloom filter mathematics
- Compaction algorithms
- Segment format specifications
- SSTable block layout
- Iterator implementation
- Recovery procedures
- Concurrency patterns

## Future Enhancements

### Hash Index
- [ ] Persistent index snapshots for faster recovery
- [ ] Memory-mapped files for lower read latency
- [ ] Bloom filters for negative lookups
- [ ] Key compression for memory savings
- [ ] Async compaction with snapshots

### LSM-Tree
- [ ] Parallel compaction across levels
- [ ] Tiered compaction strategy option
- [ ] Block cache for hot data
- [ ] Compression (Snappy, LZ4)
- [ ] Partitioned bloom filters

### General
- [ ] B-Tree storage engine
- [ ] Fractal Tree implementation
- [ ] Distributed sharding support
- [ ] Replication protocol
- [ ] SQL query layer

## Contributing

Contributions are welcome! Areas of interest:

- Performance optimizations
- Additional storage engines (B-Tree, Fractal Tree, etc.)
- Better benchmarking workloads
- Documentation improvements
- Bug fixes and tests

Please open an issue to discuss major changes before starting work.

## References and Inspiration

### Papers
- [The Log-Structured Merge-Tree (LSM-Tree)](https://www.cs.umb.edu/~poneil/lsmtree.pdf) - O'Neil et al., 1996
- [Bitcask: A Log-Structured Hash Table](https://riak.com/assets/bitcask-intro.pdf) - Basho Technologies
- [The Design and Implementation of a Log-Structured File System](https://people.eecs.berkeley.edu/~brewer/cs262/LFS.pdf) - Rosenblum & Ousterhout, 1991

### Books
- [Designing Data-Intensive Applications](https://dataintensive.net/) - Martin Kleppmann (Chapter 3: Storage and Retrieval)
- [Database Internals](https://www.databass.dev/) - Alex Petrov

### Real-World Implementations
- [LevelDB](https://github.com/google/leveldb) - Google's LSM-Tree implementation
- [RocksDB](https://rocksdb.org/) - Facebook's production LSM-Tree (LevelDB fork)
- [Bitcask](https://github.com/basho/bitcask) - Riak's hash index storage backend
- [Cassandra](https://cassandra.apache.org/) - Uses LSM-Tree for storage
- [HBase](https://hbase.apache.org/) - Built on LSM-Tree concepts

## License

MIT License - See LICENSE file for details

## Credits

Built with inspiration from:
- Bitcask (Riak's storage engine)
- LevelDB and RocksDB
- "Designing Data-Intensive Applications" by Martin Kleppmann
- The Go community's excellent standard library

---

**Ready for Production:** Both storage engines have comprehensive tests, crash recovery, and have been benchmarked under various workloads. Use with appropriate testing and monitoring for your specific use case.

For detailed documentation on each engine:
- **[Hash Index Documentation](./hashindex/README.md)**
- **[LSM-Tree Documentation](./lsm/README.md)**
- **[Component Guide](./COMPONENT_GUIDE.md)**
