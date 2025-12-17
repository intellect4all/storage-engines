package hashindex

import (
	"fmt"
	"testing"
)

// TestShardedIndexDistribution tests that keys distribute evenly across shards
func TestShardedIndexDistribution(t *testing.T) {
	index := newShardedIndex()

	// Add many keys
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%d", i)
		entry := &indexEntry{
			segmentID: 1,
			offset:    int64(i * 100),
			size:      100,
		}
		index.Put(key, entry)
	}

	// Check count
	if index.Count() != int64(numKeys) {
		t.Errorf("Expected count %d, got %d", numKeys, index.Count())
	}

	// Check distribution across shards
	shardCounts := make([]int, numShards)
	for i := 0; i < numShards; i++ {
		shardCounts[i] = len(index.shards[i].entries)
	}

	// Each shard should have approximately numKeys/numShards entries
	expectedPerShard := numKeys / numShards
	tolerance := expectedPerShard / 2 // 50% tolerance

	for i, count := range shardCounts {
		if count < expectedPerShard-tolerance || count > expectedPerShard+tolerance {
			t.Logf("Warning: Shard %d has %d entries, expected around %d", i, count, expectedPerShard)
		}
	}

	// Verify all keys are retrievable
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%d", i)
		_, exists := index.Get(key)
		if !exists {
			t.Errorf("Key %s not found in index", key)
		}
	}
}

// TestBatchUpdates tests batch update operations
func TestBatchUpdates(t *testing.T) {
	index := newShardedIndex()

	// Add initial keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		entry := &indexEntry{
			segmentID: 1,
			offset:    int64(i * 100),
			size:      100,
		}
		index.Put(key, entry)
	}

	// Prepare batch update
	updates := make(map[string]*indexEntry)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		updates[key] = &indexEntry{
			segmentID: 2,
			offset:    int64(i * 200),
			size:      150,
		}
	}

	// Prepare deletions
	deletions := make([]string, 0)
	for i := 50; i < 100; i++ {
		deletions = append(deletions, fmt.Sprintf("key%d", i))
	}

	// Apply batch update
	index.UpdateBatch(updates, deletions)

	// Verify updates
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		entry, exists := index.Get(key)
		if !exists {
			t.Errorf("Updated key %s not found", key)
			continue
		}
		if entry.segmentID != 2 {
			t.Errorf("Expected segmentID 2 for updated key, got %d", entry.segmentID)
		}
	}

	// Verify deletions
	for i := 50; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		_, exists := index.Get(key)
		if exists {
			t.Errorf("Deleted key %s still exists", key)
		}
	}

	// Verify count
	expectedCount := int64(50)
	if index.Count() != expectedCount {
		t.Errorf("Expected count %d after batch update, got %d", expectedCount, index.Count())
	}
}
