package btree

import (
	"fmt"
	"os"
	"testing"
)

func TestSimpleSplit(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-simple-split-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert enough keys to trigger a split
	// With 4KB pages and ~50 byte cells, we should be able to fit ~80 keys per page
	numKeys := 100

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		value := []byte(fmt.Sprintf("value%05d", i))
		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed at key%05d: %v", i, err)
		}

		// Verify we can immediately read it back
		retrievedValue, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed immediately after Put for key%05d: %v", i, err)
		}

		if string(retrievedValue) != string(value) {
			t.Fatalf("Value mismatch for key%05d: expected %s, got %s", i, value, retrievedValue)
		}
	}

	t.Logf("Successfully inserted and verified %d keys", numKeys)

	// Now verify all keys again
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		expectedValue := []byte(fmt.Sprintf("value%05d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Final Get failed for key%05d: %v", i, err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Final value mismatch for key%05d: expected %s, got %s", i, expectedValue, value)
		}
	}

	stats := btree.Stats()
	t.Logf("Stats: NumKeys=%d, NumPages=%d, SpaceAmp=%.2fx",
		stats.NumKeys, stats.NumSegments, stats.SpaceAmp)
}
