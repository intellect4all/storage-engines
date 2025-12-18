package btree

import (
	"fmt"
	"os"
	"testing"

	"github.com/intellect4all/storage-engines/common"
)

func setupTestBTree(t *testing.T) (*BTree, func()) {
	dir := fmt.Sprintf("/tmp/btree-test-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}

	cleanup := func() {
		btree.Close()
		os.RemoveAll(dir)
	}

	return btree, cleanup
}

func TestBasicOperations(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Test Put
	err := btree.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test Get
	value, err := btree.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(value) != "value1" {
		t.Fatalf("Expected value1, got %s", string(value))
	}

	// Test Get non-existent key
	_, err = btree.Get([]byte("nonexistent"))
	if err != common.ErrKeyNotFound {
		t.Fatalf("Expected ErrKeyNotFound, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Insert original value
	err := btree.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Update with new value
	err = btree.Put([]byte("key1"), []byte("value2"))
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify updated value
	value, err := btree.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(value) != "value2" {
		t.Fatalf("Expected value2, got %s", string(value))
	}
}

func TestDelete(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Insert and delete
	btree.Put([]byte("key1"), []byte("value1"))

	err := btree.Delete([]byte("key1"))
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, err = btree.Get([]byte("key1"))
	if err != common.ErrKeyNotFound {
		t.Fatalf("Expected ErrKeyNotFound after delete, got %v", err)
	}
}

func TestMultipleKeys(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Insert multiple keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed for key%03d: %v", i, err)
		}
	}

	// Verify all keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for key%03d: %v", i, err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Expected %s, got %s", string(expectedValue), string(value))
		}
	}
}

func TestPageSplit(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Insert enough keys to trigger page splits
	// With 4KB pages and ~128 key order, we should trigger splits
	numKeys := 1000

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		value := []byte(fmt.Sprintf("value%05d", i))
		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed for key%05d: %v", i, err)
		}
	}

	// Verify all keys are still accessible
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		expectedValue := []byte(fmt.Sprintf("value%05d", i))

		value, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for key%05d: %v", i, err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Expected %s, got %s", string(expectedValue), string(value))
		}
	}

	// Check stats
	stats := btree.Stats()
	if stats.NumKeys != int64(numKeys) {
		t.Logf("Warning: NumKeys is %d, expected %d", stats.NumKeys, numKeys)
	}

	t.Logf("Stats: NumKeys=%d, NumPages=%d, SpaceAmp=%.2fx",
		stats.NumKeys, stats.NumSegments, stats.SpaceAmp)
}

func TestPersistence(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-test-persist-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	// Create and populate btree
	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}

	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		btree.Put(key, value)
	}

	// Close
	btree.Close()

	// Reopen
	btree2, err := New(config)
	if err != nil {
		t.Fatalf("Failed to reopen btree: %v", err)
	}
	defer btree2.Close()

	// Verify all keys are still there
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree2.Get(key)
		if err != nil {
			t.Fatalf("Get failed after reopen for key%03d: %v", i, err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Expected %s, got %s", string(expectedValue), string(value))
		}
	}
}

func TestStats(t *testing.T) {
	btree, cleanup := setupTestBTree(t)
	defer cleanup()

	// Insert some data
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		btree.Put(key, value)
	}

	stats := btree.Stats()

	if stats.NumKeys != 50 {
		t.Errorf("Expected 50 keys, got %d", stats.NumKeys)
	}

	if stats.WriteCount != 50 {
		t.Errorf("Expected 50 writes, got %d", stats.WriteCount)
	}

	// Read all keys
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		btree.Get(key)
	}

	stats = btree.Stats()
	if stats.ReadCount != 50 {
		t.Errorf("Expected 50 reads, got %d", stats.ReadCount)
	}

	t.Logf("Stats: %+v", stats)
}
