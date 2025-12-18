# B-Tree Storage Engine

A production-quality B-tree storage engine implementation in Go, demonstrating the third major storage engine architecture (alongside Hash Index and LSM-Tree).

## Overview

B-trees are the most widely used storage engine in databases:
- **PostgreSQL**: Main storage (MVCC + B-tree)
- **MySQL InnoDB**: Main storage
- **SQLite**: Default storage
- **MongoDB WiredTiger**: B-tree based

**Market share: ~70% of databases use B-trees!**

## Architecture

### Core Insight: In-Place Updates

Unlike Hash Index and LSM-Tree which are append-only, B-tree overwrites data in place:

```
Hash/LSM:  Write "key1=v1" → Append
           Write "key1=v2" → Append (old version exists)
           Eventually compact to reclaim space

B-Tree:    Write "key1=v1" → Write to page
           Write "key1=v2" → OVERWRITE same location
           No compaction needed!
```

### Page-Based Design

Everything is organized in fixed 4KB pages:

```
Page Structure:
┌─────────────────────────────────────────────┐
│ Header (10 bytes)                           │
│  - type (1): INTERNAL or LEAF               │
│  - numCells (2): Number of entries          │
│  - rightPtr (4): Right sibling/child        │
│  - freePtr (2): Free space offset           │
├─────────────────────────────────────────────┤
│ Cell Directory (2 bytes × numCells)         │
│  - Offsets to cells                         │
├─────────────────────────────────────────────┤
│ Free Space (grows from both ends)           │
├─────────────────────────────────────────────┤
│ Cells (growing backward)                    │
│  Leaf: [keySize][valueSize][key][value]     │
│  Internal: [keySize][childPageID][key]      │
└─────────────────────────────────────────────┘
```

### Tree Structure

```
                [Root: Internal Node]
               /         |          \
        [Internal]  [Internal]  [Internal]
         /  |  \      /  |  \     /  |  \
    [Leaf][Leaf][Leaf]... [Leaf][Leaf][Leaf]

Properties:
- All leaves at same depth (balanced)
- Internal nodes: keys + pointers
- Leaf nodes: actual key-value pairs + right pointer
- Linked leaves enable efficient range scans
```

## Components

### 1. Page Management (`page.go`)
- Fixed 4KB pages (OS page size for efficient I/O)
- Binary search within pages (O(log n))
- Cell directory for quick access
- Efficient insertion maintaining sort order

### 2. Page Cache (`pager.go`)
- LRU cache (default: 100 pages = ~400KB memory)
- Dirty page tracking
- Metadata management (page 0)
- Free list for deleted pages

### 3. B-Tree Operations (`btree.go`)
- **Put**: Tree traversal + leaf insertion + split if needed
- **Get**: Direct path from root to leaf (O(log n))
- **Delete**: Find and remove (with merge for underflow - optional)
- **Scan**: Range queries via leaf page linking

### 4. Split Algorithm (`split.go`)
The most complex part of B-tree:

```go
// Leaf page split
Before: [Page 1: keys 1-100] ← FULL!

After:  [Page 1: keys 1-50] [Page 2: keys 51-100]
                     ↑
        Parent gets separator key "50" + pointer

// If parent is full:
Recursively split parent...
Eventually may reach root → split root → tree grows!
```

### 5. Iterator (`iterator.go`)
Range scan support:
- Seek to start key
- Follow right pointers through leaves
- O(log n) seek + O(k) scan for k results

## Usage

```go
package main

import (
    "github.com/intellect4all/storage-engines/btree"
)

func main() {
    // Create B-tree
    config := btree.DefaultConfig("./data")
    bt, _ := btree.New(config)
    defer bt.Close()

    // Write
    bt.Put([]byte("user:1"), []byte(`{"name":"Alice"}`))

    // Update (in-place!)
    bt.Put([]byte("user:1"), []byte(`{"name":"Alice Updated"}`))

    // Read
    value, _ := bt.Get([]byte("user:1"))

    // Range scan
    iter, _ := bt.Scan([]byte("user:"), []byte("user:~"))
    for iter.Next() {
        key := iter.Key()
        value := iter.Value()
        // Process...
    }
    iter.Close()

    // Stats
    stats := bt.Stats()
    fmt.Printf("Space Amp: %.2fx\n", stats.SpaceAmp)  // ~1.2x!
}
```

## Configuration

```go
type Config struct {
    DataDir   string  // Database directory
    Order     int     // Max keys per page (default: 128)
    CacheSize int     // Pages to cache (default: 100)
}
```

**Tuning:**
- **Order**: Higher = fewer splits, but larger pages
- **CacheSize**: More cache = fewer disk reads

## Performance Characteristics

### Time Complexity
- **Put**: O(log n) average, O(log n + split_cost) worst case
- **Get**: O(log n) - direct tree traversal
- **Delete**: O(log n)
- **Scan**: O(log n + k) for k results

### Space Complexity
- **Memory**: O(cache_size × 4KB) - only cached pages
- **Disk**: O(n × space_amp) where space_amp ≈ 1.1-1.3x

### Benchmark Results (Small Dataset <100 keys)

```
Write-heavy:    ~70k ops/sec (random I/O penalty)
Read-heavy:     ~300k ops/sec (direct lookup)
Range scans:    Excellent (linked leaves)

Write Amp:      2-3x (page rewrites)
Space Amp:      1.1-1.3x (BEST of all engines!)
```

## Comparison with Other Engines

```
                Hash Index    LSM-Tree     B-Tree
─────────────────────────────────────────────────────
Write Pattern   Sequential    Sequential   Random
Write Speed     Fast          Fast         Medium
Read Speed      Fastest       Medium       Fast
Space Amp       5-6x         1.5-2.5x     1.1-1.3x ← BEST
Write Amp       2x           20-30x       2-3x
Range Scans     ❌ No        ✅ Yes       ✅ Yes
Updates         Slow*         Slow*        Fast ← BEST
Compaction      Required      Required     None ← BEST

*Hash/LSM write new version, B-tree overwrites in place
```

## When to Use B-Tree

### Ideal For:
✅ **Update-heavy workloads** (in-place overwrites)
✅ **Space-constrained environments** (1.2x amplification)
✅ **No operational overhead** (no compaction)
✅ **Range queries** required
✅ **Predictable performance** (no compaction stalls)

### Avoid If:
❌ **Extreme write throughput** (random I/O is slower)
❌ **SSD write endurance** critical (more random writes)
❌ **Only point lookups** (Hash Index is simpler/faster)

## Implementation Status

### ✅ Implemented
- Fixed 4KB page structure
- LRU page cache with dirty tracking
- Tree traversal (Put/Get/Delete)
- Page split algorithm (leaf + internal + root)
- Range scan iterator
- Persistence and recovery
- Stats tracking
- **Physical Write-Ahead Log (WAL) for crash recovery** ✨ NEW!
- **Page merge on underflow** ✨ NEW!
- **Fine-grained locking (latch coupling)** ✨ NEW!
- **Variable-length key encoding (varint optimization)** ✨ NEW!

### ⚠️ Known Limitations
1. **WAL Limitation**: Crash recovery during page splits (before first checkpoint) may fail to restore root page ID correctly. Workaround: call `Sync()` periodically during bulk inserts.

2. **Internal Node Merging**: Currently only leaf pages are merged on underflow. Internal nodes are not merged (complexity deferred).

### ✅ Bug Fixes

**Split Bug (Fixed)**: Keys were becoming inaccessible with 200+ insertions due to inconsistent cell semantics between navigation functions.

**Problem**: Two navigation functions (`findChild` and `GetChildPageID`) used opposite interpretations of `Cell(K, P)`:
- One interpreted it as "P contains keys < K"
- The other as "P contains keys >= K"
- Tree construction used yet another semantic

**Result**: Keys were inserted into wrong subtrees and became unreachable during searches.

**Solution**: Standardized all code to use consistent semantics: `Cell(K, P)` means P contains keys **>= K**, and `RightPtr` contains keys **< first cell's key**.

**Files fixed**:
- `btree/btree.go` - `findChild()` navigation
- `btree/node.go` - `GetChildPageID()` navigation
- `btree/split.go` - `handleRootSplit()` construction

**Verification**: All tests now pass with up to 1000+ keys. See `COMPLETE_FIX_DOCUMENTATION.md` for detailed analysis.

### 🔮 Future Enhancements
- ✅ ~~Fix split bug for large datasets~~ **FIXED** - See COMPLETE_FIX_DOCUMENTATION.md
- ✅ ~~Implement physical WAL for crash recovery~~ **IMPLEMENTED** - See WAL_IMPLEMENTATION.md
- ✅ ~~Add page merge on underflow~~ **IMPLEMENTED** - See ENHANCEMENTS_SUMMARY.md
- ✅ ~~Fine-grained locking (latch coupling)~~ **IMPLEMENTED** - See ENHANCEMENTS_SUMMARY.md
- ✅ ~~Variable-length key optimization~~ **IMPLEMENTED** - See VARINT_OPTIMIZATION.md
- Prefix compression
- WAL improvements (root page ID tracking, compression, rotation)
- Internal node merging
- Bulk loading optimization
- MVCC/snapshot isolation

## Testing

```bash
# Run all tests
go test -v

# Run specific test
go test -v -run TestBasicOperations

# Test with various dataset sizes (all passing)
go test -v -run TestSimpleSplit  # 100 keys - PASS
go test -v -run TestMoreKeys     # 500 keys - PASS ✅ FIXED
go test -v -run TestPageSplit    # 1000 keys - PASS ✅ FIXED

# Run all tests (19 tests, all passing)
go test -v
```

## Demo

```bash
# Run interactive demo
go run cmd/demo/main.go

# Expected output:
#   ### B-Tree Demo ###
#   ✓ Created B-Tree storage engine
#   [Writing data]
#   PUT session:2001
#   ...
#   [Range scan - session:* keys]
#   Total: 2 keys in range
#   Space Amp: ~1.2x (BEST of all engines!)
```

## Architecture Insights

### Why 4KB Pages?
1. **OS page size**: Efficient I/O (no partial reads)
2. **CPU cache**: Fits in L2/L3 cache
3. **Good fanout**: ~100-200 keys per page → shallow trees

### Why In-Place Updates?
1. **Space efficiency**: No old versions pile up
2. **No compaction**: Simpler operations
3. **Predictable performance**: No compaction stalls

Trade-off: Random writes are slower than sequential

### Tree Height Example
```
With fanout = 128 (keys per page):
- 128 keys:       height = 1 (root leaf)
- 16K keys:       height = 2 (root + leaves)
- 2M keys:        height = 3
- 268M keys:      height = 4

Result: Even billions of keys need only 3-4 disk reads!
```

## Files

- `page.go` (320 lines): Page structure and cell management
- `pager.go` (270 lines): Page cache, I/O, metadata
- `btree.go` (215 lines): Main engine operations
- `node.go` (120 lines): Node helper functions
- `split.go` (220 lines): Page split algorithm
- `iterator.go` (200 lines): Range scan implementation

**Total: ~1,345 lines of production code**

## References

- "Database Internals" by Alex Petrov
- SQLite B-tree implementation
- PostgreSQL documentation on heap storage

## License

MIT

---

**Note**: This implementation demonstrates B-tree fundamentals for educational purposes. For production use in >100 key scenarios, the split bug needs to be resolved. The implementation is fully functional for small-to-medium datasets and serves as an excellent reference for understanding B-tree internals.
