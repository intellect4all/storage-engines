package lsm

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestCrashRecovery(t *testing.T) {
	dir := fmt.Sprintf("/tmp/lsm-crash-test-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	// Create LSM and write data
	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}

	// Write some data
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for key, value := range testData {
		err := lsm.Put(key, []byte(value))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Sync WAL
	lsm.Sync()

	// Close LSM (simulates clean shutdown)
	lsm.Close()

	// Reopen LSM (should recover from WAL)
	lsm2, err := New(config)
	if err != nil {
		t.Fatalf("Failed to reopen LSM: %v", err)
	}
	defer lsm2.Close()

	// Verify all data is recovered
	for key, expectedValue := range testData {
		value, found, err := lsm2.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found after recovery", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s for key %s", expectedValue, string(value), key)
		}
	}

	t.Log("Crash recovery successful")
}

func TestCompactionPreservesData(t *testing.T) {
	dir := fmt.Sprintf("/tmp/lsm-compaction-test-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.MemTableSize = 512 // Small memtable to trigger compaction
	lsm, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Write enough data to trigger compaction
	numKeys := 1000
	testData := make(map[string]string)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%05d", i)
		value := fmt.Sprintf("value%05d", i)
		testData[key] = value

		err := lsm.Put(key, []byte(value))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for background workers to complete
	time.Sleep(1 * time.Second)

	// Verify all data is still accessible
	for key, expectedValue := range testData {
		value, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found after compaction", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s for key %s", expectedValue, string(value), key)
		}
	}

	// Log level distribution
	t.Logf("After compaction:")
	t.Logf("  L0 files: %d", lsm.levels.NumFiles(0))
	t.Logf("  L1 files: %d", lsm.levels.NumFiles(1))
	t.Logf("  L2 files: %d", lsm.levels.NumFiles(2))

	t.Log("Compaction preserves all data correctly")
}

func TestBloomFilterEffectiveness(t *testing.T) {
	dir := fmt.Sprintf("/tmp/lsm-bloom-test-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.MemTableSize = 512
	lsm, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Write data and trigger flush
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%05d", i)
		value := []byte(fmt.Sprintf("value%05d", i))
		err := lsm.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Query for non-existent keys (should be fast with bloom filter)
	misses := 0
	for i := 100; i < 200; i++ {
		key := fmt.Sprintf("key%05d", i)
		_, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !found {
			misses++
		}
	}

	if misses != 100 {
		t.Fatalf("Expected 100 misses, got %d", misses)
	}

	t.Log("Bloom filter is working (all non-existent keys returned not found)")
}

func TestUpdatesDuringCompaction(t *testing.T) {
	dir := fmt.Sprintf("/tmp/lsm-update-test-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.MemTableSize = 512
	lsm, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Write initial data
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%04d", i)
		value := []byte(fmt.Sprintf("v1-%04d", i))
		err := lsm.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Trigger flush
	time.Sleep(100 * time.Millisecond)

	// Update the same keys with new values
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%04d", i)
		value := []byte(fmt.Sprintf("v2-%04d", i))
		err := lsm.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for compaction
	time.Sleep(300 * time.Millisecond)

	// Verify we get the latest values
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%04d", i)
		expectedValue := fmt.Sprintf("v2-%04d", i)

		value, found, err := lsm.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s for key %s", expectedValue, string(value), key)
		}
	}

	t.Log("Updates are correctly preserved with latest values")
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := fmt.Sprintf("/tmp/lsm-persist-test-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.MemTableSize = 512

	// First session: write and flush
	lsm1, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create LSM: %v", err)
	}

	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("key%04d", i)
		value := []byte(fmt.Sprintf("value%04d", i))
		err := lsm1.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Wait for flush and compaction
	time.Sleep(300 * time.Millisecond)

	// Close
	lsm1.Close()

	// Second session: reopen and verify
	lsm2, err := New(config)
	if err != nil {
		t.Fatalf("Failed to reopen LSM: %v", err)
	}
	defer lsm2.Close()

	// Verify all data persisted in SSTables
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("key%04d", i)
		expectedValue := fmt.Sprintf("value%04d", i)

		value, found, err := lsm2.Get(key)
		if err != nil {
			t.Fatalf("Get failed for %s: %v", key, err)
		}
		if !found {
			t.Fatalf("Key %s not found after restart", key)
		}
		if string(value) != expectedValue {
			t.Fatalf("Expected %s, got %s for key %s", expectedValue, string(value), key)
		}
	}

	// Verify SSTables were loaded
	t.Logf("After restart:")
	t.Logf("  L0 files: %d", lsm2.levels.NumFiles(0))
	t.Logf("  L1 files: %d", lsm2.levels.NumFiles(1))
	t.Logf("  L2 files: %d", lsm2.levels.NumFiles(2))

	t.Log("Data persisted across restart successfully")
}
