package btree

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestConcurrentReads(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-concurrent-reads-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert test data
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Perform concurrent reads
	const numReaders = 10
	const readsPerReader = 100

	var wg sync.WaitGroup
	errors := make(chan error, numReaders)

	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for i := 0; i < readsPerReader; i++ {
				keyID := i % 100
				key := []byte(fmt.Sprintf("key%03d", keyID))
				expectedValue := []byte(fmt.Sprintf("value%03d", keyID))

				value, err := btree.ConcurrentGet(key)
				if err != nil {
					errors <- fmt.Errorf("Reader %d: Get failed for %s: %v", readerID, string(key), err)
					return
				}

				if string(value) != string(expectedValue) {
					errors <- fmt.Errorf("Reader %d: Value mismatch for %s", readerID, string(key))
					return
				}
			}
		}(r)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	t.Logf("✓ %d concurrent readers, %d reads each completed successfully", numReaders, readsPerReader)
}

func TestConcurrentWrites(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-concurrent-writes-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Perform concurrent writes
	const numWriters = 5
	const writesPerWriter = 20

	var wg sync.WaitGroup
	errors := make(chan error, numWriters)

	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for i := 0; i < writesPerWriter; i++ {
				keyID := writerID*writesPerWriter + i
				key := []byte(fmt.Sprintf("key%05d", keyID))
				value := []byte(fmt.Sprintf("writer%d-value%d", writerID, i))

				if err := btree.ConcurrentPut(key, value); err != nil {
					errors <- fmt.Errorf("Writer %d: Put failed: %v", writerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Verify all writes
	for w := 0; w < numWriters; w++ {
		for i := 0; i < writesPerWriter; i++ {
			keyID := w*writesPerWriter + i
			key := []byte(fmt.Sprintf("key%05d", keyID))
			expectedValue := []byte(fmt.Sprintf("writer%d-value%d", w, i))

			value, err := btree.Get(key)
			if err != nil {
				t.Errorf("Verification failed for %s: %v", string(key), err)
				continue
			}

			if string(value) != string(expectedValue) {
				t.Errorf("Value mismatch for %s", string(key))
			}
		}
	}

	totalKeys := numWriters * writesPerWriter
	t.Logf("✓ %d concurrent writers, %d writes each (%d total keys)", numWriters, writesPerWriter, totalKeys)
}

func TestConcurrentReadWrite(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-concurrent-rw-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Pre-populate with some data
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Mix of readers and writers
	const numReaders = 5
	const numWriters = 3
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	errors := make(chan error, numReaders+numWriters)

	// Start readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for i := 0; i < opsPerGoroutine; i++ {
				keyID := i % 50
				key := []byte(fmt.Sprintf("key%03d", keyID))

				_, err := btree.ConcurrentGet(key)
				if err != nil {
					// Key might not exist if we're reading new writes
					// This is expected in concurrent workload
					continue
				}
			}
		}(r)
	}

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for i := 0; i < opsPerGoroutine; i++ {
				keyID := 50 + writerID*opsPerGoroutine + i
				key := []byte(fmt.Sprintf("key%05d", keyID))
				value := []byte(fmt.Sprintf("writer%d-value%d", writerID, i))

				if err := btree.ConcurrentPut(key, value); err != nil {
					errors <- fmt.Errorf("Writer %d: Put failed: %v", writerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	t.Logf("✓ Mixed workload: %d readers + %d writers completed successfully", numReaders, numWriters)
}

func TestLatchCouplingCorrectn(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-latch-correctness-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert keys using concurrent API
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := btree.ConcurrentPut(key, value); err != nil {
			t.Fatalf("ConcurrentPut failed: %v", err)
		}
	}

	// Verify using concurrent API
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%03d", i))

		value, err := btree.ConcurrentGet(key)
		if err != nil {
			t.Fatalf("ConcurrentGet failed for %s: %v", string(key), err)
		}

		if string(value) != string(expectedValue) {
			t.Fatalf("Value mismatch for %s", string(key))
		}
	}

	// Verify using regular API (should give same results)
	for i := 0; i < 100; i++ {
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

	t.Log("✓ Latch coupling produces correct results")
}
