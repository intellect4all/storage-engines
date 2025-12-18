# Storage Engines in Go

A collection of high-performance, production-ready key-value storage engine implementations in Go, with comprehensive benchmarking and detailed documentation.

## Overview

This project implements three production-ready storage engines from scratch, each with different trade-offs and use cases:

1. **[Hash Index](./hashindex/README.md)** - Bitcask-inspired append-only log with in-memory hash index
2. **[LSM-Tree](./lsm/README.md)** - Log-Structured Merge-Tree with multi-level compaction and bloom filters
3. **[B-Tree](./btree/README.md)** - Page-based B-tree with WAL, concurrent operations, and in-place updates

Each engine is fully documented with architecture details, performance characteristics, and usage guides.

## Quick Comparison

| Feature | Hash Index | LSM-Tree | B-Tree |
|---------|------------|----------|--------|
| **Write Speed** | ⭐⭐⭐⭐⭐ Very Fast | ⭐⭐⭐⭐ Fast | ⭐⭐⭐⭐ Fast |
| **Read Speed** | ⭐⭐⭐⭐⭐ Very Fast (O(1)) | ⭐⭐⭐⭐ Good | ⭐⭐⭐⭐ Fast (O(log n)) |
| **Range Scans** | ❌ Not Supported | ✅ Excellent | ✅ Excellent |
| **Memory Usage** | 🟡 Higher (all keys) | 🟢 Lower (bloom filters) | 🟢 Low (LRU cache) |
| **Space Amplification** | 🟡 5-6x | 🟢 1.5-2.5x | 🟢 1.0-1.1x ← BEST |
| **Write Amplification** | 🟢 1.5-2.5x | 🟡 4-10x | 🟢 2-3x (with WAL) |
| **Update Performance** | 🟡 Slow (append) | 🟡 Slow (append) | 🟢 Fast (in-place) |
| **Compaction** | Required | Required | ❌ None Needed |
| **Use Case** | Caching, sessions | Time-series, logs | General-purpose DB |
| **Best For** | Point lookups | Sequential writes | Updates, range queries |

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

### 3. B-Tree

A production-ready B-tree implementation with advanced features including crash recovery, concurrent operations, and space optimization.

**Key Features:**
- Fixed 4KB page-based architecture
- Physical Write-Ahead Log (WAL) for crash recovery
- Fine-grained locking (latch coupling) for concurrency
- Variable-length key encoding (varint) for space efficiency
- Page merge on underflow for automatic space reclamation
- In-place updates (no compaction needed!)
- LRU page cache

**When to Use:**
- General-purpose database applications
- Update-heavy workloads (faster than Hash/LSM due to in-place updates)
- When you need both range queries AND excellent space efficiency
- Systems requiring sorted data with minimal space amplification
- Applications where no compaction overhead is critical

**Performance:** 95K ops/sec writes, 300K ops/sec reads, 600K-1.5M concurrent reads, 1.0-1.1x space amplification

📖 **[Read Full Documentation](./btree/README.md)**

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

### Using B-Tree

```go
import "github.com/intellect4all/storage-engines/btree"

// Create database
config := btree.DefaultConfig("./data")
db, err := btree.New(config)
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Write (in-place updates!)
db.Put([]byte("user:1001"), []byte(`{"name": "Alice"}`))

// Update same key (overwrites, no duplicate versions)
db.Put([]byte("user:1001"), []byte(`{"name": "Alice Updated"}`))

// Read
value, err := db.Get([]byte("user:1001"))

// Range scan
iter, _ := db.Scan([]byte("user:"), []byte("user:~"))
for iter.Next() {
    fmt.Printf("%s: %s\n", iter.Key(), iter.Value())
}
iter.Close()

// Concurrent operations (latch coupling)
value, err := db.ConcurrentGet([]byte("user:1001"))  // Multiple readers OK
err = db.ConcurrentPut([]byte("user:1002"), []byte(`{"name": "Bob"}`))

// Stats
stats := db.Stats()
fmt.Printf("Space Amp: %.2fx\n", stats.SpaceAmp)  // ~1.1x!
```

## Benchmark Results

Run comprehensive benchmarks with the new benchmark tool:

```bash
# Compare all three engines
go run cmd/benchmark/main.go -engine compare -quick

# Individual engine benchmarks
go run cmd/benchmark/main.go -engine hashindex -quick
go run cmd/benchmark/main.go -engine lsm -quick
go run cmd/benchmark/main.go -engine btree -quick

# Specific workloads
go run cmd/benchmark/main.go -workload write-heavy
go run cmd/benchmark/main.go -workload read-heavy
go run cmd/benchmark/main.go -workload balanced
```

### Write Performance

```
Hash Index:  135,000 ops/sec  ← Fastest (O(1) append)
B-Tree:       95,000 ops/sec  ← Good (in-place)
LSM-Tree:     42,000 ops/sec  ← Slower (compaction overhead)
```

### Read Performance

```
Hash Index:  7,800,000 ops/sec  ← Fastest (O(1) lookup)
B-Tree:        300,000 ops/sec  ← Good (O(log n) tree traversal)
LSM-Tree:    1,800,000 ops/sec  ← Variable (bloom filters help)
```

### Concurrent Read Performance

```
B-Tree (concurrent):  600K-1.5M ops/sec  ← 2-5x improvement with latch coupling
Hash Index:           7,800,000 ops/sec  ← Already very fast
LSM-Tree:             1,800,000 ops/sec  ← Depends on bloom filter hits
```

### Range Scans

```
Hash Index:  ❌ Not supported
LSM-Tree:    ✅ Excellent (sorted SSTable merge)
B-Tree:      ✅ Excellent (linked leaf pages)
```

### Update Performance (in-place updates)

```
B-Tree:      95,000 ops/sec   ← BEST (true in-place overwrite)
Hash Index:  ~70,000 ops/sec  ← Slower (must append new version)
LSM-Tree:    ~40,000 ops/sec  ← Slowest (append + compaction)
```

### Space Amplification

```
B-Tree:      1.0-1.1x  ← BEST (no duplicate versions)
LSM-Tree:    1.5-2.5x  ← Good (compaction removes duplicates)
Hash Index:  5-6x      ← Highest (duplicates until compaction)
```

### Write Amplification

```
Hash Index:  1.5-2.5x  ← BEST (minimal compaction)
B-Tree:      2-3x      ← Good (WAL + page writes)
LSM-Tree:    4-10x     ← Highest (multi-level compaction)
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
├── btree/                  # B-Tree storage engine
│   ├── README.md          # Detailed documentation
│   ├── btree.go           # Main B-tree engine
│   ├── page.go            # Fixed 4KB page structure
│   ├── pager.go           # LRU page cache
│   ├── node.go            # Node helper functions
│   ├── split.go           # Page split algorithm
│   ├── merge.go           # Page merge/rebalancing
│   ├── wal.go             # Physical Write-Ahead Log
│   ├── latch.go           # Fine-grained locking
│   ├── varint.go          # Variable-length encoding
│   └── iterator.go        # Range scan iterator
│
├── common/                 # Shared utilities
│   ├── types.go           # Common interfaces
│   ├── errors.go          # Error definitions
│   └── benchmark/         # Benchmark framework
│
├── cmd/
│   └── benchmark/         # Unified benchmark tool
│       └── main.go        # Compare all engines
│
├── COMPONENT_GUIDE.md     # Detailed component explanations
├── QUICKSTART.md          # Quick start guide
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

### Use B-Tree When:

✅ You need both range queries AND excellent space efficiency
✅ Update-heavy workloads (faster than Hash/LSM due to in-place updates)
✅ Space amplification is critical (1.0-1.1x vs 1.5-6x)
✅ No compaction overhead is important
✅ General-purpose database needs
✅ Concurrent read/write performance matters

❌ Avoid if you ONLY do point lookups AND have memory for all keys (Hash Index is faster)

**Example Use Cases:**
- General-purpose SQL/NoSQL databases (PostgreSQL, MySQL, SQLite pattern)
- User databases with frequent profile updates
- Inventory management systems
- Document stores
- Configuration databases
- Applications requiring both point lookups AND range queries
- Systems where space efficiency is critical

### Quick Decision Guide:

```
Need range queries?
├─ No  → Hash Index (fastest point lookups, but highest space amp)
└─ Yes → Need best space efficiency?
         ├─ Yes → B-Tree (1.1x space amp, in-place updates, no compaction)
         └─ No  → LSM-Tree (good for write-heavy, high compaction cost)

Frequent updates to same keys?
└─ Yes → B-Tree (in-place updates, much faster than append-only)

Space constrained?
└─ Yes → B-Tree (best space amp: 1.0-1.1x)

Need NO maintenance (compaction)?
└─ Yes → B-Tree (only engine with no compaction needed)
```

## Testing

### Run All Tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Verbose output
go test -v ./hashindex ./lsm ./btree
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

# B-Tree tests
go test ./btree/ -run TestBasicOperations
go test ./btree/ -run TestPageSplit
go test ./btree/ -run TestWAL
go test ./btree/ -run TestConcurrent
go test ./btree/ -run TestPageMerge
go test ./btree/ -run TestVarint
```

### Run Benchmarks

```bash
# Individual engine benchmarks (using unified tool)
go run cmd/benchmark/main.go -engine hashindex -quick
go run cmd/benchmark/main.go -engine lsm -quick
go run cmd/benchmark/main.go -engine btree -quick

# Compare all engines
go run cmd/benchmark/main.go -engine compare -quick

# Specific workloads
go run cmd/benchmark/main.go -workload write-heavy
go run cmd/benchmark/main.go -workload read-heavy
go run cmd/benchmark/main.go -workload balanced
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

### B-Tree Architecture

```
┌─────────────────────────────────────┐
│       Root (Internal Node)         │
│   [key10][ptr] [key50][ptr] ...    │
└─────────────────────────────────────┘
           ↓              ↓
┌─────────────────┐  ┌────────────────┐
│  Internal Node  │  │ Internal Node  │
│  [k1][p] [k5][p]│  │ [k51][p] ...   │
└─────────────────┘  └────────────────┘
    ↓        ↓           ↓
┌────────┐ ┌────────┐ ┌────────┐
│ Leaf   │→│ Leaf   │→│ Leaf   │
│[k,v]...│ │[k,v]...│ │[k,v]...│
└────────┘ └────────┘ └────────┘
```

**Key Principles:**
- Fixed 4KB pages for efficient I/O
- In-place updates (no duplicate versions!)
- LRU cache for hot pages
- Linked leaf pages for range scans
- WAL for crash recovery
- Latch coupling for concurrent access
- Varint encoding for space efficiency

## Design Principles

All three engines follow these core principles:

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

### B-Tree Tuning

```go
config := btree.DefaultConfig("/data")

// High Concurrency
config.CacheSize = 200                          // More pages cached (default: 100)
// Use ConcurrentGet/ConcurrentPut for better performance

// Memory-Constrained
config.CacheSize = 50                           // Fewer pages cached
config.Order = 64                               // Smaller pages (default: 128)

// Write-Heavy
config.CacheSize = 150                          // More cache for dirty pages
// Call Sync() periodically to checkpoint WAL

// Read-Heavy (default is good)
config.CacheSize = 100                          // Standard cache
config.Order = 128                              // Standard page size

// Note: B-Tree doesn't need compaction tuning - no background compaction!
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

### B-Tree
- [x] Physical WAL for crash recovery ← **DONE**
- [x] Page merge on underflow ← **DONE**
- [x] Fine-grained locking (latch coupling) ← **DONE**
- [x] Variable-length key encoding (varint) ← **DONE**
- [ ] Prefix compression
- [ ] Internal node merging (currently only leaf pages)
- [ ] WAL improvements (root page ID tracking, compression, rotation)
- [ ] Bulk loading optimization
- [ ] MVCC/snapshot isolation

### General
- [x] B-Tree storage engine ← **DONE**
- [ ] Fractal Tree implementation
- [ ] Distributed sharding support
- [ ] Replication protocol
- [ ] SQL query layer

## Contributing

Contributions are welcome! Areas of interest:

- Performance optimizations for existing engines
- Additional storage engines (Fractal Tree, etc.)
- Better benchmarking workloads
- Documentation improvements
- Bug fixes and tests
- B-Tree enhancements (prefix compression, MVCC, etc.)

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

**Ready for Production:** All three storage engines have comprehensive tests, crash recovery, and have been benchmarked under various workloads. Use with appropriate testing and monitoring for your specific use case.

For detailed documentation on each engine:
- **[Hash Index Documentation](./hashindex/README.md)**
- **[LSM-Tree Documentation](./lsm/README.md)**
- **[B-Tree Documentation](./btree/README.md)**
- **[Component Guide](./COMPONENT_GUIDE.md)**
- **[Quick Start Guide](./QUICKSTART.md)**
