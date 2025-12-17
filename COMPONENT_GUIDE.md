# Storage Engines: Comprehensive Component Guide

This document provides a deep dive into each component of the three storage engines implemented in this project.

## Table of Contents
1. [LSM-Tree Components](#lsm-tree-components)
2. [Hash Index Components](#hash-index-components)
3. [SSTable Format](#sstable-format)
4. [Common Concepts](#common-concepts)

---

## LSM-Tree Components

### 1. MemTable (`lsm/memtable.go`)

**Purpose**: In-memory sorted structure for recent writes before they're flushed to disk.

**Data Structure**:
```go
type MemTableEntry struct {
    Key      string
    Value    []byte
    Sequence uint64  // Monotonically increasing, determines version
    Deleted  bool     // Tombstone marker
}
```

**Implementation Details**:
- Uses a **sorted slice** with binary search (simple and effective)
- **Thread-safe** with RWMutex (multiple readers, single writer)
- Entries maintained in **sorted order** by key
- **Sequence numbers** provide total ordering of all writes

**Why Sorted Slice Instead of Tree?**
```
Alternatives considered:
1. Red-Black Tree: O(log n) insert, O(log n) search
   - Pros: Balanced, predictable
   - Cons: Complex, cache-unfriendly

2. Skip List: O(log n) average insert/search
   - Pros: Simpler than RB-tree, concurrent-friendly
   - Cons: Probabilistic, variable height

3. Sorted Slice: O(n) insert, O(log n) search
   - Pros: Cache-friendly, simple, fast iteration
   - Cons: Slow inserts for large sizes

Choice: Sorted slice because memtable is small (4MB),
flushed frequently, and iteration is fast (needed for flush).
```

**Operations**:
```go
// Put: O(n) insertion (binary search + shift)
Put(key, value, seq) -> Updates existing or inserts new

// Get: O(log n) binary search
Get(key) -> (value, sequence, deleted, found)

// GetAllEntries: O(1) (already sorted!)
GetAllEntries() -> []MemTableEntry  // For flushing
```

**Memory Management**:
- Size tracked approximately: `len(key) + len(value) + 16 bytes overhead`
- Becomes immutable when `size >= maxSize` (default 4MB)
- Old entries replaced (not appended) to save space

---

### 2. Write-Ahead Log (`lsm/wal.go`)

**Purpose**: Provide durability for writes before they're persisted to SSTables.

**Record Format**:
```
[CRC32: 4 bytes]      - Checksum for corruption detection
[Sequence: 8 bytes]   - Global sequence number
[KeySize: 4 bytes]    - Length of key
[ValueSize: 4 bytes]  - Length of value
[Deleted: 1 byte]     - Tombstone flag (0 or 1)
[Key: variable]       - Actual key bytes
[Value: variable]     - Actual value bytes
```

**Why This Format?**
- **CRC first**: Detect corruption before parsing
- **Sizes before data**: Know how much to read
- **Self-contained**: Each record independent

**Recovery Process**:
```
1. On startup, read WAL from beginning to end
2. For each record:
   a. Validate CRC
   b. If valid, apply to memtable
   c. Track max sequence number
3. Resume from max sequence + 1
4. Keep WAL until memtable flushed
5. Delete old WAL, create new one
```

**Crash Scenarios**:
```
Scenario 1: Crash before memtable flush
- WAL has all writes
- Recovery replays WAL → memtable reconstructed
- Result: No data loss ✅

Scenario 2: Crash during memtable flush
- Partial SSTable written
- Recovery replays WAL → memtable reconstructed
- Re-flush memtable → new SSTable
- Result: Duplicate SSTable (cleaned up by compaction)

Scenario 3: Crash after memtable flush, before WAL delete
- SSTable persisted
- WAL still exists
- Recovery loads SSTable + replays WAL
- Result: Duplicate entries (sequence numbers deduplicate)
```

---

### 3. Bloom Filter (`lsm/bloom.go`)

**Purpose**: Probabilistic data structure to skip SSTables that definitely don't contain a key.

**Math Behind It**:
```
Given:
- n = expected number of keys
- p = desired false positive rate (e.g., 0.01 for 1%)

Optimal parameters:
- m (number of bits) = -(n * ln(p)) / (ln(2)²)
- k (number of hashes) = (m/n) * ln(2)

Example with n=1000, p=0.01:
- m = -(1000 * ln(0.01)) / 0.48 ≈ 9585 bits (1.2 KB)
- k = (9585/1000) * 0.69 ≈ 7 hash functions
```

**Implementation**:
```go
// Uses double hashing for efficiency
h_i(x) = (h1(x) + i * h2(x)) mod m

// Instead of k different hash functions, compute just 2:
h1 = FNV64a(key)
h2 = FNV64(key)

// Then derive k hashes via linear combination
```

**Why Double Hashing?**
- **Efficient**: Only 2 hash computations instead of k
- **Good distribution**: Linear combination spreads bits well
- **Fast**: FNV is a fast non-cryptographic hash

**Performance Impact**:
```
Without Bloom Filter:
Get("nonexistent_key"):
- Check L0: 4 files × 100µs = 400µs
- Check L1: 10 files × 100µs = 1000µs
- Check L2: 100 files × 100µs = 10000µs
Total: ~11.4ms per negative lookup!

With Bloom Filter (1% FP):
- Bloom checks: 5 levels × 1µs = 5µs
- 99% stop here (negative result)
- 1% false positive → 11.4ms
Average: 5µs + 0.01 * 11400µs = 119µs

Speedup: 96x faster! 🚀
```

---

### 4. SSTable Structure (`lsm/sstable.go`)

**Purpose**: Immutable sorted file on disk with efficient search.

**File Layout**:
```
┌─────────────────────────────────────┐
│ Data Block 1 (4KB)                  │ ← Sorted entries
├─────────────────────────────────────┤
│ Data Block 2 (4KB)                  │
├─────────────────────────────────────┤
│ ...                                  │
├─────────────────────────────────────┤
│ Data Block N (4KB)                  │
├─────────────────────────────────────┤
│ Index Block                         │ ← Maps first key → block offset
├─────────────────────────────────────┤
│ Metadata Block                      │ ← MinKey, MaxKey
├─────────────────────────────────────┤
│ Bloom Filter                        │ ← Membership test
├─────────────────────────────────────┤
│ Footer (28 bytes)                   │ ← Offsets + magic number
└─────────────────────────────────────┘
```

**Data Block Format**:
```
[NumEntries: 4 bytes]
[Entry1]
[Entry2]
...

Entry format:
[KeySize: 4 bytes]
[ValueSize: 4 bytes]
[Deleted: 1 byte]
[Key: variable]
[Value: variable]
```

**Index Block Format**:
```
[NumEntries: 4 bytes]
[IndexEntry1]
[IndexEntry2]
...

IndexEntry format:
[KeySize: 4 bytes]
[BlockOffset: 8 bytes]  ← Position of data block in file
[Key: variable]         ← First key in that block
```

**Search Algorithm**:
```go
func Get(key string) (value []byte, found bool, err error) {
    // Step 1: Check bloom filter (1µs)
    if !bloomFilter.MayContain(key) {
        return nil, false, nil  // Definitely not present
    }

    // Step 2: Binary search index to find block (log n)
    blockIdx := binarySearch(index, key)

    // Step 3: Read block from disk (100µs)
    block := readBlock(blockIdx)

    // Step 4: Linear search within block (10µs)
    return searchBlock(block, key)
}
```

**Why 4KB Blocks?**
```
Alternatives:
- 512 bytes: Too small, index overhead too high
- 64KB: Too large, waste I/O for small values

4KB is optimal because:
1. Matches page size (efficient I/O)
2. ~100 entries per block (good granularity)
3. Small enough for fast linear search within block
4. Large enough to amortize index overhead
```

---

### 5. SSTable Builder (`lsm/sstable_builder.go`)

**Purpose**: Construct immutable SSTables from sorted entries.

**Build Process**:
```
1. Initialize:
   - Create file
   - Init bloom filter (based on expected keys)
   - Init current block buffer

2. For each entry (must be in sorted order!):
   - Add key to bloom filter
   - Track minKey (first key) and maxKey (last key)
   - Append entry to current block
   - If block full (>4KB):
     a. Flush block to disk
     b. Add index entry (first key → block offset)
     c. Reset block buffer

3. Finish:
   - Flush remaining block
   - Write index block
   - Write metadata block (minKey, maxKey)
   - Write bloom filter
   - Write footer (offsets + magic)
   - Sync to disk
   - Close file
```

**Critical Invariant**: Entries MUST be added in sorted order!
```
Why? Because we build index as we go:
- First key of each block goes in index
- If unsorted, index would be wrong
- Result: Binary search would fail!

Enforcement: Caller's responsibility (flush, compaction)
```

**Block Flushing**:
```go
// When block exceeds 4KB:
1. Write block header (numEntries)
2. Write block to file
3. Pad to 4KB boundary (for alignment)
4. Create index entry:
   - Key: first key in block
   - Offset: file position of block start
5. Reset block buffer
```

---

### 6. Compaction (`lsm/compaction.go`)

**Purpose**: Merge sorted files to reclaim space and reduce read amplification.

**K-Way Merge Algorithm**:
```
Problem: Merge N sorted files into 1 sorted file

Algorithm:
1. Create iterator for each input file
2. Build min-heap with first entry from each iterator
3. While heap not empty:
   a. Pop smallest entry (by key)
   b. If duplicate key, keep entry with highest sequence
   c. Write entry to output file
   d. Advance iterator that produced this entry
   e. Push next entry from that iterator to heap

Time: O(k * n * log k) where k=files, n=total entries
Space: O(k) for heap
```

**Handling Duplicates**:
```
Example: Same key in multiple files
File 1: key="user1", seq=5, value="Alice"
File 2: key="user1", seq=8, value="Bob"
File 3: key="user1", seq=3, value="Charlie"

Merge process:
1. All 3 appear in heap simultaneously
2. Heap orders by (key, -sequence):
   ("user1", -8)  ← Pop this (highest sequence)
   ("user1", -5)
   ("user1", -3)
3. Write ("user1", seq=8, "Bob")
4. Skip ("user1", seq=5) and ("user1", seq=3)

Result: Only latest version kept!
```

**Tombstone Handling**:
```
Rule: Drop tombstones ONLY in final level (L4)

Why?
- Tombstone in L2 might delete key in L3/L4
- If dropped too early, old version resurfaces!

Example:
L1: ("user1", DELETED, seq=10)
L2: ("user1", "Alice", seq=5)

If we drop tombstone in L1→L2 compaction:
Result: ("user1", "Alice") ← WRONG! Should be deleted

Correct: Keep tombstone until L4
```

**Compaction Strategies**:

**L0 → L1** (Special Case):
```
Problem: L0 files may overlap (from memtable flushes)
Solution: Merge ALL L0 files at once

Example:
L0-001: [a-m, z]     ← Overlaps!
L0-002: [b-n, x]     ← Overlaps!

L0→L1 compaction:
- Pick ALL L0 files
- Find overlapping L1 files
- Merge all together
- Output: Non-overlapping L1 files
```

**L1 → L2, L2 → L3, L3 → L4** (Leveled):
```
Strategy: Pick 1 file from source, merge with overlapping target files

Example L1→L2:
L1: [a-m], [n-z]
L2: [a-f], [g-m], [n-s], [t-z]

Step 1: Pick L1 file [a-m]
Step 2: Find overlapping L2 files: [a-f], [g-m]
Step 3: Merge these 3 files
Step 4: Output: New L2 files [a-m] (merged and compacted)

Result:
- Removed 3 old files ([a-m] from L1, [a-f] and [g-m] from L2)
- Created 1 new file ([a-m] in L2)
- Reclaimed space from deleted/overwritten keys
```

---

### 7. Level Manager (`lsm/levels.go`)

**Purpose**: Organize SSTables across multiple levels and track when compaction needed.

**Level Configuration**:
```go
L0: 40 MB    (4-10 files)         ← May overlap
L1: 400 MB   (10-100 files)       ← Non-overlapping
L2: 4 GB     (100-1000 files)     ← Non-overlapping
L3: 40 GB    (1000-10000 files)   ← Non-overlapping
L4: 400 GB   (10000+ files)       ← Non-overlapping
```

**Why 10x Size Ratio?**
```
Write amplification = 1 + (size_ratio × num_levels)

With 10x ratio and 4 levels:
WA = 1 + (10 × 4) = 41x

Seems high, but alternatives worse:
- 5x ratio, 6 levels: WA = 1 + (5 × 6) = 31x (more levels to check)
- 20x ratio, 2 levels: WA = 1 + (20 × 2) = 41x (huge compactions)

10x is industry standard (LevelDB, RocksDB)
```

**Compaction Triggers**:
```
L0: Trigger when >= 4 files (count-based)
    Why? L0 files overlap, more files = slower reads

L1+: Trigger when size >= max_size (size-based)
     Why? Non-overlapping, size matters more than count
```

**File Organization**:
```
L0: Unsorted (by time created)
    - Don't sort, order doesn't matter
    - All checked anyway (overlap)

L1-L4: Sorted by minKey
       - Binary search possible
       - Skip non-overlapping files
```

---

### 8. Main LSM Engine (`lsm/lsm.go`)

**Purpose**: Coordinate all components, handle reads/writes, manage background workers.

**Write Path**:
```
Put(key, value):
1. Generate sequence number (atomic increment)
2. Append to WAL (for durability)
3. Insert into active memtable
4. If memtable full:
   a. Lock
   b. Swap active ← new empty memtable
   c. Set immutable ← old active
   d. Signal flush worker (non-blocking)
   e. Unlock
5. Return (fast!)
```

**Read Path**:
```
Get(key):
1. Check active memtable (fastest)
   - If found, return immediately

2. Check immutable memtable (if exists)
   - If found, return immediately

3. Check L0 SSTables (ALL of them, may overlap)
   - For each: bloom filter → binary search
   - If found, return immediately

4. Check L1 SSTables (binary search by key range)
   - Find files where minKey <= key <= maxKey
   - For each: bloom filter → binary search
   - If found, return immediately

5. Repeat for L2, L3, L4

6. Not found → return nil

Read amplification: Worst case checks all levels
```

**Background Workers**:

**Flush Worker**:
```go
for {
    select {
    case <-flushChan:
        if immutableMemtable != nil {
            1. Get all entries from immutable memtable
            2. Build SSTable from entries
            3. Add SSTable to L0
            4. Delete WAL (data now durable in SSTable)
            5. Set immutableMemtable = nil
            6. Check if L0 needs compaction
        }
    case <-closeChan:
        return
    }
}
```

**Compaction Worker**:
```go
for {
    select {
    case <-compactionChan:
        if L0.needsCompaction() {
            compactL0ToL1()
            triggerNextIfNeeded(L1)
        } else if L1.needsCompaction() {
            compactL1ToL2()
            triggerNextIfNeeded(L2)
        } else if L2.needsCompaction() {
            compactL2ToL3()
            triggerNextIfNeeded(L3)
        } else if L3.needsCompaction() {
            compactL3ToL4()
        }
    case <-closeChan:
        return
    }
}
```

---

## Hash Index Components

### 1. Sharded Index (`hashindex/shard.go`)

**Purpose**: Divide keyspace into independent shards to reduce lock contention.

**Architecture**:
```
256 shards (power of 2 for fast modulo)
Each shard independently manages:
- In-memory index (map[uint64]Location)
- Active segment
- List of immutable segments
- Own lock (RWMutex)

Shard selection: hash(key) % 256
```

**Why 256 Shards?**
```
Trade-offs:
- 16 shards: Simple, but contention with >16 threads
- 256 shards: Sweet spot for modern CPUs (8-16 cores)
- 1024 shards: Overhead, diminishing returns

256 chosen because:
1. Enough parallelism for 16-32 threads
2. Low overhead (256 × small struct = <1MB)
3. Power of 2 (fast modulo with bitwise AND)
```

---

### 2. Segment Format (`hashindex/segment.go`)

**Entry Format**:
```
[CRC32: 4 bytes]      - Checksum
[Timestamp: 8 bytes]  - Unix timestamp
[KeySize: 4 bytes]    - Length of key
[ValueSize: 4 bytes]  - Length of value
[Key: variable]       - Key bytes
[Value: variable]     - Value bytes
```

**Append-Only Writes**:
```
Benefits:
1. Sequential I/O (fast!)
2. No random seeks
3. Simple crash recovery
4. No corruption from partial writes

Cost:
- Space amplification (old values not deleted)
- Need compaction
```

---

### 3. Compaction (`hashindex/compaction.go`)

**Strategy**: Merge-rewrite all segments into one.

```
Process:
1. Scan all segments
2. Build map: key → latest value
3. Write map to new segment (sorted order)
4. Atomically replace old segments
5. Update in-memory index

Problem: Blocks writes during merge!
Solution: LSM-Tree (incremental compaction)
```

---

## SSTable Format

### Block-Based Storage

**Why Blocks?**
```
Alternative 1: One entry per disk read
- Pro: Simple
- Con: 100 entries = 100 disk seeks = 10ms!

Alternative 2: Entire file in memory
- Pro: Fast
- Con: Doesn't scale

Block-based (4KB blocks):
- Pro: Amortizes disk seeks
- Pro: Fits in page cache
- Pro: Small enough for fast search
- Balance: ~100 entries per block
```

---

## Common Concepts

### Sequence Numbers

**Purpose**: Total ordering of all writes for conflict resolution.

```
Timeline:
t=1: Put("user1", "Alice") → seq=1
t=2: Put("user2", "Bob")   → seq=2
t=3: Put("user1", "Carol") → seq=3
t=4: Delete("user2")       → seq=4

During merge:
- "user1" has seq=1 and seq=3 → Keep seq=3 ("Carol")
- "user2" has seq=2 and seq=4 → Keep seq=4 (deleted)
```

**Implementation**:
```go
var sequence uint64  // Global atomic counter

func Put(key, value) {
    seq := atomic.AddUint64(&sequence, 1)
    entry := Entry{key, value, seq, deleted=false}
    ...
}
```

---

### Tombstones

**Purpose**: Mark keys as deleted without immediate removal.

**Why Needed?**
```
Problem without tombstones:
1. Delete "key1" from memtable
2. Flush memtable (no entry for "key1")
3. "key1" still in L1 SSTable!
4. Get("key1") returns old value ← BUG!

Solution with tombstones:
1. Delete "key1" → Add tombstone to memtable
2. Flush memtable → Tombstone in L0
3. Compaction merges L0+L1
4. Tombstone overrides old value
5. Drop tombstone in final level (L4)
```

**Format**:
```go
type Entry struct {
    Key     string
    Value   []byte
    Deleted bool    ← Tombstone marker
    Seq     uint64
}
```

---

### Write Amplification

**Definition**: Total bytes written to disk / User bytes written

```
Example:
User writes 100MB of data
Compaction rewrites it 3 times
Total disk writes: 100MB + 300MB = 400MB
Write amplification: 400MB / 100MB = 4x
```

**LSM Write Amplification Calculation**:
```
For LSM with 10x ratio and 4 levels:

L0→L1: Write 40MB, read 40MB (L0) + 400MB (L1) = 440MB write
L1→L2: Write 400MB, read 400MB (L1) + 4GB (L2) = 4.4GB write
L2→L3: Write 4GB, read 4GB (L2) + 40GB (L3) = 44GB write
L3→L4: Write 40GB, read 40GB (L3) + 400GB (L4) = 440GB write

Total amplification ≈ 1 + 10 × num_levels = 41x
```

---

### Space Amplification

**Definition**: Actual disk usage / Logical data size

```
Example:
Logical data: 100MB (latest versions)
Disk usage: 250MB (includes old versions, tombstones)
Space amplification: 250MB / 100MB = 2.5x
```

**Factors**:
1. Old versions not yet compacted
2. Tombstones not yet dropped
3. Fragmentation in blocks
4. Bloom filters, indexes, metadata

---

### Crash Recovery

**WAL-Based Recovery**:
```
On Startup:
1. Check if WAL exists
2. If yes:
   a. Replay all entries into memtable
   b. Restore sequence number
3. Load existing SSTables
4. Resume operation

Guarantees:
- Durability: All synced writes recovered
- Consistency: Sequence numbers maintain order
- Isolation: No partial writes visible
```

---

## Performance Tuning Guide

### MemTable Size
```
Small (1MB):
+ Faster flush
+ Less memory
- More L0 files
- More compaction

Large (64MB):
+ Fewer L0 files
+ Less compaction
- Slower flush
- More memory
- Longer WAL replay

Sweet spot: 4-8MB
```

### Block Size
```
Small (512B):
+ Fine-grained access
- Large index overhead
- More seeks

Large (64KB):
+ Small index
+ Fewer seeks
- Wasted I/O
- Slower search

Sweet spot: 4KB (page size)
```

### Bloom Filter False Positive Rate
```
Lower FP (0.1%):
+ Fewer false positives
+ Fewer wasted reads
- Larger bloom filter
- More memory

Higher FP (5%):
+ Smaller bloom filter
+ Less memory
- More false positives
- More wasted reads

Sweet spot: 1% (balanced)
```

---

## Debugging Tips

### Check Level Distribution
```go
for level := 0; level < 5; level++ {
    numFiles := lsm.levels.NumFiles(level)
    size := lsm.levels.LevelSize(level)
    fmt.Printf("L%d: %d files, %d MB\n", level, numFiles, size/1024/1024)
}
```

### Monitor Write Amplification
```
Track:
- Bytes written by user
- Bytes written by compaction
- Ratio = total / user

High ratio (>50x)?
→ Level sizes too small
→ Increase size ratio or thresholds
```

### Find Hot Keys
```
Add read counter to SSTable:
- Which SSTables get most reads?
- Which blocks get most reads?
- Optimize: Cache hot blocks in memory
```

---

**This guide provides the foundation for understanding and extending these storage engines. Each design decision represents a trade-off optimized for specific workloads.**
