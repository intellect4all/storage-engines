package btree

import (
	"fmt"
	"os"
	"testing"
)

func TestIteratorBasic(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-iter-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert some keys
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		btree.Put(key, value)
	}

	// Scan all keys
	iter, err := btree.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	defer iter.Close()

	count := 0
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		expectedKey := fmt.Sprintf("key%03d", count)
		expectedValue := fmt.Sprintf("value%03d", count)

		if string(key) != expectedKey {
			t.Fatalf("Expected key %s, got %s", expectedKey, string(key))
		}

		if string(value) != expectedValue {
			t.Fatalf("Expected value %s, got %s", expectedValue, string(value))
		}

		count++
	}

	if iter.Error() != nil {
		t.Fatalf("Iterator error: %v", iter.Error())
	}

	if count != 50 {
		t.Fatalf("Expected 50 keys, got %d", count)
	}

	t.Logf("Successfully scanned %d keys", count)
}

func TestIteratorRange(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-iter-range-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		btree.Put(key, value)
	}

	// Scan range [key020, key030)
	startKey := []byte("key020")
	endKey := []byte("key030")

	iter, err := btree.Scan(startKey, endKey)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	defer iter.Close()

	count := 0
	expectedStart := 20
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		expectedKey := fmt.Sprintf("key%03d", expectedStart+count)
		expectedValue := fmt.Sprintf("value%03d", expectedStart+count)

		if string(key) != expectedKey {
			t.Fatalf("Expected key %s, got %s", expectedKey, string(key))
		}

		if string(value) != expectedValue {
			t.Fatalf("Expected value %s, got %s", expectedValue, string(value))
		}

		count++
	}

	if iter.Error() != nil {
		t.Fatalf("Iterator error: %v", iter.Error())
	}

	expectedCount := 10 // key020 through key029
	if count != expectedCount {
		t.Fatalf("Expected %d keys, got %d", expectedCount, count)
	}

	t.Logf("Successfully scanned %d keys in range", count)
}
