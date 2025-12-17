package hashindex

import (
	"hash/fnv"
	"runtime"
	"sync"
	"sync/atomic"
)

const (
	// Number of shards for the index map (power of 2 for efficient modulo)
	numShards = 256
	shardMask = numShards - 1
)

// indexEntry represents a key's location in a segment
type indexEntry struct {
	segmentID int
	offset    int64
	size      int32
	timestamp int64
}

// shard is a single partition of the index map
type shard struct {
	mu      sync.RWMutex
	entries map[string]*indexEntry
}

// shardedIndex is a concurrent hash map with fine-grained locking
type shardedIndex struct {
	shards [numShards]*shard
	count  atomic.Int64 // Total number of keys
}

func newShardedIndex() *shardedIndex {
	si := &shardedIndex{}
	for i := 0; i < numShards; i++ {
		si.shards[i] = &shard{
			entries: make(map[string]*indexEntry),
		}
	}
	return si
}

// getShard returns the shard for a given key
func (si *shardedIndex) getShard(key string) *shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()
	return si.shards[hash&shardMask]
}

func (si *shardedIndex) Get(key string) (*indexEntry, bool) {
	shard := si.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	entry, exists := shard.entries[key]
	return entry, exists
}

func (si *shardedIndex) Put(key string, entry *indexEntry) {
	shard := si.getShard(key)
	shard.mu.Lock()
	_, existed := shard.entries[key]
	shard.entries[key] = entry
	shard.mu.Unlock()

	if !existed {
		si.count.Add(1)
	}
}

func (si *shardedIndex) Delete(key string) bool {
	shard := si.getShard(key)
	shard.mu.Lock()
	_, existed := shard.entries[key]
	delete(shard.entries, key)
	shard.mu.Unlock()

	if existed {
		si.count.Add(-1)
	}
	return existed
}

// Count returns the total number of keys
func (si *shardedIndex) Count() int64 {
	return si.count.Load()
}

// UpdateBatch atomically updates multiple entries
// Used during compaction to replace entries for compacted segments
func (si *shardedIndex) UpdateBatch(updates map[string]*indexEntry, deletions []string) {

	type batchOp struct {
		updates   map[string]*indexEntry
		deletions []string
	}

	shardOps := make([]batchOp, numShards)
	for i := 0; i < numShards; i++ {
		shardOps[i].updates = make(map[string]*indexEntry)
		shardOps[i].deletions = make([]string, 0)
	}

	// Distribute operations to shards
	for k, v := range updates {
		h := fnv.New32a()
		h.Write([]byte(k))
		hash := h.Sum32()
		idx := hash & shardMask
		shardOps[idx].updates[k] = v
	}

	for _, k := range deletions {
		h := fnv.New32a()
		h.Write([]byte(k))
		hash := h.Sum32()
		idx := hash & shardMask
		shardOps[idx].deletions = append(shardOps[idx].deletions, k)
	}

	// Apply operations in parallel with atomic counter
	var deltaCount atomic.Int64
	wg := sync.WaitGroup{}
	for i := 0; i < numShards; i++ {
		wg.Add(1)
		go func(shard *shard, shardOps *batchOp) {
			defer wg.Done()

			shard.mu.Lock()
			defer shard.mu.Unlock()

			localDelta := int64(0)

			// Apply updates
			for k, v := range shardOps.updates {
				_, existed := shard.entries[k]
				shard.entries[k] = v
				if !existed {
					localDelta++
				}
			}

			// Apply deletions
			for _, k := range shardOps.deletions {
				_, existed := shard.entries[k]
				delete(shard.entries, k)
				if existed {
					localDelta--
				}
			}

			if localDelta != 0 {
				deltaCount.Add(localDelta)
			}
		}(si.shards[i], &shardOps[i])

		// Yield to other goroutines every few shards
		if i%16 == 0 {
			runtime.Gosched()
		}
	}

	wg.Wait()
	si.count.Add(deltaCount.Load())
}
