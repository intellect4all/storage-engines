package testutil

import (
	"os"
	"testing"
)

// TempDir creates a temporary directory for testing
func TempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "storage-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}
