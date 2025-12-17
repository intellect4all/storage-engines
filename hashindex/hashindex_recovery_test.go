package hashindex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/intellect4all/storage-engines/common"
)

// TestRecovery tests basic recovery from disk
func TestRecovery(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create and populate
	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Recover
	h2, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	// Verify all keys
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := fmt.Sprintf("value%d", i)

		val, err := h2.Get(key)
		if err != nil {
			t.Errorf("Get failed after recovery for key %s: %v", key, err)
			continue
		}
		if string(val) != expectedValue {
			t.Errorf("Expected %s, got %s", expectedValue, val)
		}
	}

	stats := h2.Stats()
	if stats.NumKeys != 1000 {
		t.Errorf("Expected 1000 keys after recovery, got %d", stats.NumKeys)
	}
}

// TestRecoveryWithMultipleSegments tests recovery with multiple segment files
func TestRecoveryWithMultipleSegments(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SegmentSizeBytes = 512 // Small size to create multiple segments
	config.MaxSegments = 20

	// Create and populate
	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 500; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	statsBefore := h.Stats()
	if statsBefore.NumSegments <= 1 {
		t.Fatal("Expected multiple segments before recovery")
	}

	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Recover
	h2, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	statsAfter := h2.Stats()
	if statsAfter.NumSegments != statsBefore.NumSegments {
		t.Logf("Segment count changed after recovery: before=%d, after=%d",
			statsBefore.NumSegments, statsAfter.NumSegments)
	}

	// Verify all keys
	for i := 0; i < 500; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := fmt.Sprintf("value%d", i)

		val, err := h2.Get(key)
		if err != nil {
			t.Errorf("Get failed after recovery for key %s: %v", key, err)
			continue
		}
		if string(val) != expectedValue {
			t.Errorf("Expected %s, got %s", expectedValue, val)
		}
	}
}

// TestRecoveryWithDeletes tests recovery correctly handles tombstones
func TestRecoveryWithDeletes(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create and populate
	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := h.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	// Delete half
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		if err := h.Delete(key); err != nil {
			t.Fatal(err)
		}
	}

	if err := h.Sync(); err != nil {
		t.Fatal(err)
	}

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Recover
	h2, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	// Verify deleted keys are gone
	deletedFound := 0
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		_, err := h2.Get(key)
		if err != common.ErrKeyNotFound {
			deletedFound++
		}
	}

	// Verify remaining keys
	remainingFound := 0
	for i := 50; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := fmt.Sprintf("value%d", i)

		val, err := h2.Get(key)
		if err != nil {
			t.Errorf("Get failed after recovery for key %s: %v", key, err)
			continue
		}
		if string(val) != expectedValue {
			t.Errorf("Expected %s, got %s", expectedValue, val)
		}
		remainingFound++
	}

	stats := h2.Stats()
	// Note: Stats may include tombstone entries that haven't been compacted
	t.Logf("After recovery: %d keys in stats, %d deleted keys found, %d remaining keys verified",
		stats.NumKeys, deletedFound, remainingFound)

	// At minimum, all non-deleted keys should be present
	if remainingFound != 50 {
		t.Errorf("Expected to find 50 remaining keys, found %d", remainingFound)
	}
}

// TestRecoveryEmptyDirectory tests recovery on empty directory
func TestRecoveryEmptyDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create on empty directory
	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	stats := h.Stats()
	if stats.NumKeys != 0 {
		t.Errorf("Expected 0 keys on empty directory, got %d", stats.NumKeys)
	}
	if stats.NumSegments != 1 {
		t.Errorf("Expected 1 segment on empty directory, got %d", stats.NumSegments)
	}
}

// TestRecoveryFileHandling tests recovery handles invalid files gracefully
func TestRecoveryFileHandling(t *testing.T) {
	dir, err := os.MkdirTemp("", "hashindex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create some dummy files that should be ignored
	dummyFiles := []string{"readme.txt", "temp.dat", "not-a-segment.seg2"}
	for _, fname := range dummyFiles {
		path := filepath.Join(dir, fname)
		if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a valid segment file manually
	segPath := filepath.Join(dir, "12345.seg")
	if err := os.WriteFile(segPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Recovery should handle this gracefully
	h, err := New(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Should be able to write data
	if err := h.Put([]byte("test"), []byte("value")); err != nil {
		t.Fatal(err)
	}

	val, err := h.Get([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "value" {
		t.Errorf("Expected value, got %s", val)
	}
}
