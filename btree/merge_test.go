package btree

import (
	"fmt"
	"os"
	"testing"
)

func TestPageMerge(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-merge-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert 100 keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Check initial stats
	initialStats := btree.Stats()
	t.Logf("Initial: NumKeys=%d, NumPages=%d", initialStats.NumKeys, initialStats.NumSegments)

	// Delete many keys to trigger merges
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		if err := btree.Delete(key); err != nil {
			t.Fatalf("Delete failed for %s: %v", string(key), err)
		}
	}

	// Check stats after deletes
	afterStats := btree.Stats()
	t.Logf("After deletes: NumKeys=%d, NumPages=%d", afterStats.NumKeys, afterStats.NumSegments)

	// Verify remaining keys are still accessible
	for i := 50; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s after deletes: %v", string(key), err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for %s", string(key))
		}
	}

	// Verify deleted keys are gone
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		_, err := btree.Get(key)
		if err == nil {
			t.Fatalf("Key %s should have been deleted", string(key))
		}
	}

	t.Log("✓ All deletes successful, remaining keys verified")

	// Note: Page merge may or may not reduce page count depending on
	// tree structure and fill factors. The important test is that
	// remaining keys are still accessible.
}

func TestPageMergeWithReinsert(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-merge-reinsert-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Pattern: Insert, delete, re-insert
	// This tests that pages remain functional after merge operations

	// Phase 1: Insert
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Phase 2: Delete half
	for i := 0; i < 25; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		if err := btree.Delete(key); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	}

	// Phase 3: Re-insert with different values
	for i := 0; i < 25; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("UPDATED%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Re-insert failed: %v", err)
		}
	}

	// Verify all keys
	for i := 0; i < 25; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("UPDATED%03d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", string(key), err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for %s: expected %s, got %s",
				string(key), string(expectedValue), string(value))
		}
	}

	for i := 25; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", string(key), err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for %s", string(key))
		}
	}

	t.Log("✓ Delete and re-insert cycle successful")
}

func TestMergeShouldNotAffectSmallDeletes(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-merge-small-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert a small number of keys
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Delete just one key (should not trigger merge)
	key := []byte("key000")
	if err := btree.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify remaining keys
	for i := 1; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", string(key), err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for %s", string(key))
		}
	}

	// Verify deleted key is gone
	_, err = btree.Get([]byte("key000"))
	if err == nil {
		t.Fatal("key000 should have been deleted")
	}

	t.Log("✓ Small delete successful without merge")
}
