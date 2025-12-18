# Quick Start Guide

Get up and running with all three storage engines in 2 minutes!

## 1. Clone and Build

```bash
# Clone repository
git clone https://github.com/intellect4all/storage-engines
cd storage-engines

# Initialize dependencies
go mod tidy

# Build benchmark tool
go build -o benchmark ./cmd/benchmark
```

## 2. Run Quick Comparison

```bash
#Compare all three engines
./benchmark -engine compare -quick
```

**Expected Output:**
```
Storage Engine Benchmark Suite
================================
Duration: 10s
Concurrency: 8
Mode: compare

=== Comparing HashIndex vs. LSM-Tree vs. B-Tree ===

Running write-heavy workload...
  HashIndex:  135,000 ops/sec  (Write P99: 180µs)
  LSM-Tree:    42,000 ops/sec  (Write P99: 420µs)
  B-Tree:      95,000 ops/sec  (Write P99: 250µs)

Running read-heavy workload...
  HashIndex:  7,800,000 ops/sec  (Read P99: 8µs)
  LSM-Tree:   1,800,000 ops/sec  (Read P99: 15µs)
  B-Tree:       300,000 ops/sec  (Read P99: 45µs)

Running balanced workload...
  HashIndex:  290,000 ops/sec
  LSM-Tree:    85,000 ops/sec
  B-Tree:     120,000 ops/sec

Space Amplification:
  HashIndex:  5.8x  ← Highest (duplicates until compaction)
  LSM-Tree:   1.9x  ← Good (compaction removes duplicates)
  B-Tree:     1.1x  ← BEST! (in-place updates)
```

## 3. Test Individual Engines

### Hash Index (Fastest Point Lookups)

```bash
./benchmark -engine hashindex -quick
```

**What to Expect:**
- ⚡ **300K+ ops/sec** in mixed workloads
- 🟢 **O(1) lookups** - Hash index magic
- ❌ **No range scans** - Only point lookups

**Use Case:** Session stores, user profiles, caching

### LSM-Tree (Best for Sequential Writes)

```bash
./benchmark -engine lsm -quick
```

**What to Expect:**
- ✅ **Range scans** - Excellent for time-series
- 📊 **45K write ops/sec** - Good write throughput
- 🔍 **Bloom filters** - Skip non-existent keys fast

**Use Case:** Time-series data, log aggregation, analytics

### B-Tree (Best Space Efficiency)

```bash
./benchmark -engine btree -quick
```

**What to Expect:**
- 🎯 **1.1x space amp** - BEST of all three engines!
- ⚡ **95K write ops/sec** - In-place updates
- ✅ **Range scans** - Linked leaf pages
- 🔄 **No compaction** - Zero maintenance overhead!

**Use Case:** General-purpose databases, frequent updates, space-constrained systems

## 4. Try Specific Workloads

```bash
# Write-heavy workload
./benchmark -workload write-heavy -engine compare

# Read-heavy workload
./benchmark -workload read-heavy -engine compare

# Balanced workload
./benchmark -workload balanced -engine compare
```

## 5. Use in Your Code

### Hash Index - Fastest Point Lookups

```go
import "github.com/intellect4all/storage-engines/hashindex"

config := hashindex.DefaultConfig("./data")
db, _ := hashindex.New(config)
defer db.Close()

// Write
db.Put([]byte("user:1001"), []byte(`{"name":"Alice"}`))

// Read (O(1) hash lookup!)
value, _ := db.Get([]byte("user:1001"))

// Update (appends new version)
db.Put([]byte("user:1001"), []byte(`{"name":"Alice Updated"}`))
```

### LSM-Tree - Best for Range Queries

```go
import "github.com/intellect4all/storage-engines/lsm"

config := lsm.DefaultConfig("./data")
db, _ := lsm.New(config)
defer db.Close()

// Write
db.Put("user:1001", []byte(`{"name":"Alice"}`))

// Read
value, found, _ := db.Get("user:1001")

// Range scan (sorted iteration!)
iter := db.Scan("user:1000", "user:2000")
for iter.Valid() {
    fmt.Printf("%s: %s\n", iter.Key(), iter.Value())
    iter.Next()
}
```

### B-Tree - Best Space Efficiency

```go
import "github.com/intellect4all/storage-engines/btree"

config := btree.DefaultConfig("./data")
db, _ := btree.New(config)
defer db.Close()

// Write (in-place update!)
db.Put([]byte("user:1001"), []byte(`{"name":"Alice"}`))

// Update same key (overwrites, no duplicate!)
db.Put([]byte("user:1001"), []byte(`{"name":"Alice Updated"}`))

// Read
value, _ := db.Get([]byte("user:1001"))

// Range scan
iter, _ := db.Scan([]byte("user:1000"), []byte("user:2000"))
for iter.Next() {
    fmt.Printf("%s: %s\n", iter.Key(), iter.Value())
}
iter.Close()

// Concurrent operations (latch coupling!)
value, _ := db.ConcurrentGet([]byte("user:1001"))  // 2-5x faster
db.ConcurrentPut([]byte("user:1002"), []byte(`{"name":"Bob"}`))
```

## 6. Understanding the Results

### Throughput

```
HashIndex:  290,000 ops/sec  ← Fastest (O(1) hash)
B-Tree:     120,000 ops/sec  ← Good (O(log n) tree)
LSM-Tree:    85,000 ops/sec  ← Slower (multi-level compaction)
```

Higher is better!

### Latency Percentiles

```
Write Latency:
  P50:  4.8µs   ← 50% of writes complete in this time
  P95:  8.4µs   ← 95% of writes
  P99:  805µs   ← 99% of writes (tail latency)
```

- **P50** (median) - typical case
- **P99** - what most users experience
- **P999** - worst case

### Space Amplification

```
B-Tree:      1.1x  ← BEST! (in-place updates)
LSM-Tree:    1.9x  ← Good (compaction)
HashIndex:   5.8x  ← Highest (duplicates)
```

**1.0x is perfect** - B-Tree is closest!

### Write Amplification

```
HashIndex:   1.5-2.5x  ← BEST (minimal compaction)
B-Tree:      2-3x      ← Good (WAL + pages)
LSM-Tree:    4-10x     ← Highest (multi-level compaction)
```

Lower is better!

## 7. Quick Decision Guide

**Choose Hash Index if:**
- ✅ Need maximum throughput (300K+ ops/sec)
- ✅ Only point lookups (no range scans)
- ✅ All keys fit in memory
- ✅ Can afford 5-6x space amplification

**Choose LSM-Tree if:**
- ✅ Need range queries
- ✅ Write-heavy workload
- ✅ Dataset larger than memory
- ✅ Time-series or log data

**Choose B-Tree if:**
- ✅ Need range queries AND best space efficiency
- ✅ Frequent updates to same keys
- ✅ Space constrained (1.1x amp!)
- ✅ Don't want compaction overhead
- ✅ General-purpose database needs

## 8. Run Tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific engine tests
go test ./hashindex/ -v
go test ./lsm/ -v
go test ./btree/ -v

# Specific features
go test ./btree/ -run TestWAL -v          # WAL crash recovery
go test ./btree/ -run TestConcurrent -v   # Latch coupling
go test ./btree/ -run TestPageMerge -v    # Space reclamation
go test ./btree/ -run TestVarint -v       # Variable-length encoding
```

## 9. Advanced Benchmarking

```bash
# Custom duration and concurrency
./benchmark -engine compare -duration 60s -concurrency 16

# Test different concurrency levels
./benchmark -engine btree -quick -concurrency 1   # Single-threaded
./benchmark -engine btree -quick -concurrency 8   # Multi-threaded
./benchmark -engine btree -quick -concurrency 32  # High contention

# Individual workloads
./benchmark -engine hashindex -workload write-heavy
./benchmark -engine lsm -workload read-heavy
./benchmark -engine btree -workload balanced
```

## 10. Key Takeaways

### Hash Index
- **300K+ ops/sec** - Fastest point lookups
- **O(1) access** - Hash table advantage
- **5-6x space amp** - Highest overhead
- ❌ No range scans

### LSM-Tree
- **45K write ops/sec** - Good for sequential writes
- **Range scans** - Sorted SSTable merge
- **1.9x space amp** - Compaction helps
- **4-10x write amp** - Multi-level compaction cost

### B-Tree
- **95K write ops/sec** - In-place updates
- **1.1x space amp** - BEST of all three!
- **Range scans** - Linked leaf pages
- **2-3x write amp** - WAL overhead
- **No compaction** - Zero maintenance!

## 11. CLI Reference

```bash
./benchmark [OPTIONS]

Options:
  -engine string
        Engine to benchmark: hashindex, lsm, btree, or compare (default: compare)
  -quick
        Run quick benchmarks (10s each, fewer keys)
  -workload string
        Workload to run: all, write-heavy, read-heavy, balanced (default: all)
  -duration duration
        Duration for each benchmark (default: 60s)
  -concurrency int
        Number of concurrent workers (default: 8)

Examples:
  ./benchmark -engine compare -quick                    # Compare all three
  ./benchmark -engine btree -quick                      # Test B-Tree only
  ./benchmark -workload write-heavy -engine compare     # Write-heavy comparison
  ./benchmark -duration 30s -concurrency 16             # Custom settings
```

## 12. Next Steps

- 📖 Read [README.md](README.md) for full project overview
- 📚 Read [COMPONENT_GUIDE.md](COMPONENT_GUIDE.md) for deep dive
- 🔍 Explore individual engine documentation:
  - [Hash Index](./hashindex/README.md)
  - [LSM-Tree](./lsm/README.md)
  - [B-Tree](./btree/README.md)
- 🛠️ Try implementing your own optimizations!

---

**Ready to benchmark? Start with:**
```bash
go mod tidy
go build -o benchmark ./cmd/benchmark
./benchmark -engine compare -quick
```

**Compare all three engines in 30 seconds!** ⚡
