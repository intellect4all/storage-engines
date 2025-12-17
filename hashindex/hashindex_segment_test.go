package hashindex

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestSegmentRotation tests that segments rotate at the configured size
func TestSegmentRotation(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SegmentSizeBytes = 256 // Small size to force rotation
	config.MaxSegments = 10       // Don't trigger compaction yet

	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	initialStats := h.Stats()
	if initialStats.NumSegments != 1 {
		t.Errorf("Expected 1 initial segment, got %d", initialStats.NumSegments)
	}

	// Write enough data to trigger rotation
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	// Wait a bit for any background operations
	time.Sleep(100 * time.Millisecond)

	stats := h.Stats()
	if stats.NumSegments <= 1 {
		t.Errorf("Expected multiple segments after rotation, got %d", stats.NumSegments)
	}

	// Verify all data is still accessible
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := fmt.Sprintf("value%d", i)
		val, err := h.Get(key)
		if err != nil {
			t.Errorf("Get failed for key %s: %v", key, err)
			continue
		}
		if string(val) != expectedValue {
			t.Errorf("Expected %s, got %s", expectedValue, val)
		}
	}
}
