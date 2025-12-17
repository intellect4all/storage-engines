package hashindex

import (
	"bytes"
	"os"
	"testing"

	"github.com/intellect4all/storage-engines/common"
)

// TestBasicOperations tests basic CRUD operations
func TestBasicOperations(t *testing.T) {
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

	// Test Put
	if err := h.Put([]byte("key1"), []byte("value1")); err != nil {
		t.Fatal(err)
	}

	// Test Get
	val, err := h.Get([]byte("key1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}

	// Test Update
	if err := h.Put([]byte("key1"), []byte("value2")); err != nil {
		t.Fatal(err)
	}

	val, err = h.Get([]byte("key1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "value2" {
		t.Errorf("Expected value2, got %s", val)
	}

	// Test Delete
	if err := h.Delete([]byte("key1")); err != nil {
		t.Fatal(err)
	}

	_, err = h.Get([]byte("key1"))
	if err != common.ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound, got %v", err)
	}
}

// TestEdgeCases tests various edge cases and error conditions
func TestEdgeCases(t *testing.T) {
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

	// Test empty key
	err = h.Put([]byte(""), []byte("value"))
	if err != common.ErrKeyEmpty {
		t.Errorf("Expected ErrKeyEmpty for empty key, got %v", err)
	}

	// Test nil value (tombstone)
	if err := h.Put([]byte("key1"), []byte("value1")); err != nil {
		t.Fatal(err)
	}
	if err := h.Put([]byte("key1"), nil); err != nil {
		t.Fatal(err)
	}
	_, err = h.Get([]byte("key1"))
	if err != common.ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after nil value, got %v", err)
	}

	// Test empty value
	if err := h.Put([]byte("key2"), []byte("")); err != nil {
		t.Fatal(err)
	}
	_, err = h.Get([]byte("key2"))
	if err != common.ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after empty value, got %v", err)
	}

	// Test non-existent key
	_, err = h.Get([]byte("nonexistent"))
	if err != common.ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound for non-existent key, got %v", err)
	}

	// Test large key and value
	largeKey := bytes.Repeat([]byte("k"), 1024)
	largeValue := bytes.Repeat([]byte("v"), 1024*1024) // 1MB
	if err := h.Put(largeKey, largeValue); err != nil {
		t.Fatal(err)
	}
	val, err := h.Get(largeKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(val, largeValue) {
		t.Error("Large value mismatch")
	}
}

// TestClosedStorage verifies operations on closed storage engine return errors
func TestClosedStorage(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}

	// Put some data
	if err := h.Put([]byte("key1"), []byte("value1")); err != nil {
		t.Fatal(err)
	}

	// Close the storage
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Test operations on closed storage
	err = h.Put([]byte("key2"), []byte("value2"))
	if err != common.ErrClosed {
		t.Errorf("Expected ErrClosed on Put, got %v", err)
	}

	_, err = h.Get([]byte("key1"))
	if err != common.ErrClosed {
		t.Errorf("Expected ErrClosed on Get, got %v", err)
	}

	err = h.Delete([]byte("key1"))
	if err != common.ErrClosed {
		t.Errorf("Expected ErrClosed on Delete, got %v", err)
	}

	err = h.Sync()
	if err != common.ErrClosed {
		t.Errorf("Expected ErrClosed on Sync, got %v", err)
	}

	// Test double close
	if err := h.Close(); err != nil {
		t.Errorf("Double close should not error, got %v", err)
	}
}

// TestSync tests synchronization operations
func TestSync(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SyncOnWrite = true // Enable sync on every write

	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Write with SyncOnWrite enabled
	for i := 0; i < 10; i++ {
		key := []byte("key")
		value := []byte("value")
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	// Manual sync should not error
	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}
}
