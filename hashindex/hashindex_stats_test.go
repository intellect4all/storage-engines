package hashindex

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestStats tests statistics tracking
func TestStats(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SegmentSizeBytes = 512
	config.MaxSegments = 5

	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Write some data
	numKeys := 100
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	// Update some keys to increase write amp
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("updated-value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	// Read all keys
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		if _, err := h.Get(key); err != nil {
			t.Fatal(err)
		}
	}

	// Wait for potential compaction
	time.Sleep(300 * time.Millisecond)

	stats := h.Stats()

	// Verify stats
	if stats.NumKeys != int64(numKeys) {
		t.Errorf("Expected %d keys, got %d", numKeys, stats.NumKeys)
	}

	if stats.WriteCount != int64(numKeys+50) {
		t.Errorf("Expected %d writes, got %d", numKeys+50, stats.WriteCount)
	}

	if stats.ReadCount != int64(numKeys) {
		t.Errorf("Expected %d reads, got %d", numKeys, stats.ReadCount)
	}

	if stats.TotalDiskSize <= 0 {
		t.Error("Expected positive disk size")
	}

	if stats.WriteAmp < 1.0 {
		t.Errorf("Write amplification should be at least 1.0, got %.2f", stats.WriteAmp)
	}

	if stats.SpaceAmp < 1.0 {
		t.Errorf("Space amplification should be at least 1.0, got %.2f", stats.SpaceAmp)
	}

	t.Logf("Stats: NumKeys=%d, NumSegments=%d, WriteAmp=%.2f, SpaceAmp=%.2f, CompactCount=%d",
		stats.NumKeys, stats.NumSegments, stats.WriteAmp, stats.SpaceAmp, stats.CompactCount)
}
