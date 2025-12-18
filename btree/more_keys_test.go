package btree

import (
	"fmt"
	"os"
	"testing"
)

func TestMoreKeys(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-more-keys-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	numKeys := 500

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		value := []byte(fmt.Sprintf("value%05d", i))
		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed at key%05d: %v", i, err)
		}

		if i%50 == 0 {
			t.Logf("Inserted %d keys", i)
		}
	}

	t.Logf("All %d keys inserted", numKeys)

	// Verify all keys
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		expectedValue := []byte(fmt.Sprintf("value%05d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for key%05d: %v", i, err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for key%05d", i)
		}

		if i%50 == 0 {
			t.Logf("Verified %d keys", i)
		}
	}

	stats := btree.Stats()
	t.Logf("Final stats: NumKeys=%d, NumPages=%d", stats.NumKeys, stats.NumSegments)
}
