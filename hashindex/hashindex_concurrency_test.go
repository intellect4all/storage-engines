package hashindex

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/intellect4all/storage-engines/common"
)

// TestConcurrency tests concurrent write operations
func TestConcurrency(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SegmentSizeBytes = 1024 * 1024 // 1MB for faster rotation

	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	numWorkers := 10
	numOpsPerWorker := 1000

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOpsPerWorker; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", workerID, j))
				value := []byte(fmt.Sprintf("value-%d-%d", workerID, j))
				if err := h.Put(key, value); err != nil {
					t.Errorf("Put failed: %v", err)
					break
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all keys
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < numOpsPerWorker; j++ {
			key := []byte(fmt.Sprintf("key-%d-%d", i, j))
			expectedValue := fmt.Sprintf("value-%d-%d", i, j)

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

	stats := h.Stats()
	if stats.NumKeys != int64(numWorkers*numOpsPerWorker) {
		t.Errorf("Expected %d keys, got %d", numWorkers*numOpsPerWorker, stats.NumKeys)
	}

	t.Logf("Stats: %d keys, %d segments, %d compactions", stats.NumKeys, stats.NumSegments, stats.CompactCount)
}

// TestConcurrentReadsAndWrites tests concurrent mixed read/write workload
func TestConcurrentReadsAndWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Preload some data
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			counter := 0
			for {
				select {
				case <-stopChan:
					return
				default:
					key := []byte(fmt.Sprintf("key%d", counter%100))
					value := []byte(fmt.Sprintf("writer%d-value%d", workerID, counter))
					h.Put(key, value)
					counter++
				}
			}
		}(i)
	}

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0
			for {
				select {
				case <-stopChan:
					return
				default:
					key := []byte(fmt.Sprintf("key%d", counter%100))
					_, err := h.Get(key)
					if err != nil && err != common.ErrKeyNotFound {
						t.Errorf("Unexpected error during concurrent read: %v", err)
					}
					counter++
				}
			}
		}()
	}

	// Let them run for a bit
	time.Sleep(2 * time.Second)
	close(stopChan)
	wg.Wait()

	stats := h.Stats()
	t.Logf("Concurrent R/W stats: %d reads, %d writes", stats.ReadCount, stats.WriteCount)
}
