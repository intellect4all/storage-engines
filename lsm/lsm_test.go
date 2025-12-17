package lsm

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func setupTestLSM(t *testing.T) (*LSM, func()) {
	dir := fmt.Sprintf("/tmp/lsm-test-%d", time.Now().UnixNano())
	config := DefaultConfig(dir)
	config.MemTableSize = 1024 // Small memtable for testing

	lsm, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}

	cleanup := func() {
		lsm.Close()
		os.RemoveAll(dir)
	}

	return lsm, cleanup
}

func TestBasicOperations(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Test Put and Get
	err := lsm.Put("key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	value, found, err := lsm.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("Key not found")
	}
	if string(value) != "value1" {
		t.Fatalf("Expected value1, got %s", string(value))
	}

	// Test non-existent key
	_, found, err = lsm.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("Non-existent key found")
	}
}

func TestDelete(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Put a key
	err := lsm.Put("key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists
	_, found, err := lsm.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("Key not found")
	}

	// Delete it
	err = lsm.Delete("key1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, found, err = lsm.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("Deleted key still found")
	}
}

func TestUpdate(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Put original value
	err := lsm.Put("key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Update with new value
	err = lsm.Put("key1", []byte("value2"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify updated value
	value, found, err := lsm.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("Key not found")
	}
	if string(value) != "value2" {
		t.Fatalf("Expected value2, got %s", string(value))
	}
}

func TestMemtableFlush(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Write enough data to trigger flush
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%04d", i)
		value := []byte(fmt.Sprintf("value%04d", i))
		err := lsm.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for flush to complete
	time.Sleep(100 * time.Millisecond)

	// Verify data is still accessible
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%04d", i)
		expectedValue := fmt.Sprintf("value%04d", i)

		value, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s", expectedValue, string(value))
		}
	}

	// Check that L0 has files
	numL0Files := lsm.levels.NumFiles(0)
	if numL0Files == 0 {
		t.Fatal("Expected L0 files after flush")
	}
	t.Logf("L0 has %d files", numL0Files)
}

func TestL0Compaction(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Write enough data to trigger multiple flushes and compaction
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key%04d", i)
		value := []byte(fmt.Sprintf("value%04d", i))
		err := lsm.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for flushes and compaction
	time.Sleep(500 * time.Millisecond)

	// Verify data is still accessible
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key%04d", i)
		expectedValue := fmt.Sprintf("value%04d", i)

		value, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s", expectedValue, string(value))
		}
	}

	// Check level distribution
	t.Logf("L0 files: %d", lsm.levels.NumFiles(0))
	t.Logf("L1 files: %d", lsm.levels.NumFiles(1))
	t.Logf("L2 files: %d", lsm.levels.NumFiles(2))
}

func TestRangeScan(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Insert sorted data
	keys := []string{"a", "b", "c", "d", "e"}
	for _, key := range keys {
		err := lsm.Put(key, []byte("value_"+key))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Scan all keys
	iter := lsm.Scan("", "")
	var scannedKeys []string
	for iter.Valid() {
		scannedKeys = append(scannedKeys, iter.Key())
		iter.Next()
	}

	if len(scannedKeys) != len(keys) {
		t.Fatalf("Expected %d keys, got %d", len(keys), len(scannedKeys))
	}

	for i, key := range keys {
		if scannedKeys[i] != key {
			t.Fatalf("Expected key %s at position %d, got %s", key, i, scannedKeys[i])
		}
	}
}

func TestTombstones(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Put and delete keys
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%04d", i)
		err := lsm.Put(key, []byte("value"))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Delete even keys
	for i := 0; i < 10; i += 2 {
		key := fmt.Sprintf("key%04d", i)
		err := lsm.Delete(key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	}

	// Verify odd keys exist, even keys don't
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%04d", i)
		_, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if i%2 == 0 {
			if found {
				t.Fatalf("Deleted key %s still found", key)
			}
		} else {
			if !found {
				t.Fatalf("Key %s not found", key)
			}
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	lsm, cleanup := setupTestLSM(t)
	defer cleanup()

	// Write concurrently from multiple goroutines
	done := make(chan bool)
	for g := 0; g < 10; g++ {
		go func(id int) {
			for i := 0; i < 50; i++ {
				key := fmt.Sprintf("key%02d%04d", id, i)
				value := []byte(fmt.Sprintf("value%d", i))
				err := lsm.Put(key, value)
				if err != nil {
					t.Errorf("Put failed: %v", err)
				}
			}
			done <- true
		}(g)
	}

	// Wait for all writes
	for g := 0; g < 10; g++ {
		<-done
	}

	// Wait for background workers
	time.Sleep(200 * time.Millisecond)

	// Verify all data
	for g := 0; g < 10; g++ {
		for i := 0; i < 50; i++ {
			key := fmt.Sprintf("key%02d%04d", g, i)
			expectedValue := fmt.Sprintf("value%d", i)

			value, found, err := lsm.Get(key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}
			if !found {
				t.Fatalf("Key %s not found", key)
			}
			if string(value) != expectedValue {
				t.Fatalf("Expected %s, got %s", expectedValue, string(value))
			}
		}
	}

	t.Logf("Successfully wrote and verified %d keys", 10*50)
}
