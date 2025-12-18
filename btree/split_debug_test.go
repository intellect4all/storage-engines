package btree

import (
	"fmt"
	"os"
	"testing"
)

func TestSplitDebug(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-split-debug-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert keys one by one and verify each
	numKeys := 150
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		value := []byte(fmt.Sprintf("value%05d", i))

		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed at key%05d: %v", i, err)
		}

		// Verify we can immediately read it back
		retrievedValue, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed immediately after Put for key%05d: %v", i, err)
		}

		if string(retrievedValue) != string(value) {
			t.Fatalf("Value mismatch for key%05d", i)
		}

		// Every 10 keys, verify all previous keys are still accessible
		if i > 0 && i%10 == 0 {
			for j := 0; j <= i; j++ {
				testKey := []byte(fmt.Sprintf("key%05d", j))
				testValue := []byte(fmt.Sprintf("value%05d", j))

				val, err := btree.Get(testKey)
				if err != nil {
					t.Fatalf("Verification failed at key%05d (after inserting %d keys): %v", j, i, err)
				}

				if string(val) != string(testValue) {
					t.Fatalf("Value mismatch for key%05d after inserting %d keys", j, i)
				}
			}
			t.Logf("✓ Verified all keys up to key%05d", i)
		}
	}

	t.Logf("✓ Successfully inserted and verified all %d keys", numKeys)
}

func TestDumpTree(t *testing.T) {
	dir := fmt.Sprintf("/tmp/btree-dump-%d", os.Getpid())
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	btree, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create btree: %v", err)
	}
	defer btree.Close()

	// Insert enough keys to trigger splits
	numKeys := 429
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		value := []byte(fmt.Sprintf("value%05d", i))
		err := btree.Put(key, value)
		if err != nil {
			t.Fatalf("Put failed at key%05d: %v", i, err)
		}
	}

	// Verify all keys are accessible
	t.Logf("Verifying all %d keys before dump...", numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		_, err := btree.Get(key)
		if err != nil {
			t.Fatalf("Get failed for key%05d: %v", i, err)
		}
	}
	t.Logf("✓ All keys verified")

	// Dump tree structure
	t.Logf("Tree structure:")
	dumpTree(t, btree, btree.pager.RootPageID(), 0)
}

func dumpTree(t *testing.T, btree *BTree, pageID uint32, depth int) {
	page, err := btree.pager.GetPage(pageID)
	if err != nil {
		t.Logf("%sError loading page %d: %v", indent(depth), pageID, err)
		return
	}

	numCells := page.NumCells()
	pageType := "LEAF"
	if !page.IsLeaf() {
		pageType = "INTERNAL"
	}

	t.Logf("%sPage %d [%s]: %d cells, rightPtr=%d", indent(depth), pageID, pageType, numCells, page.RightPtr())

	if page.IsLeaf() {
		// Show first and last keys in leaf
		if numCells > 0 {
			firstCell, _ := page.CellAt(0)
			lastCell, _ := page.CellAt(numCells - 1)
			t.Logf("%s  Keys: %s ... %s", indent(depth), string(firstCell.Key), string(lastCell.Key))
		}
	} else {
		// Recurse into children
		for i := uint16(0); i < numCells; i++ {
			cell, err := page.CellAt(i)
			if err != nil {
				continue
			}
			t.Logf("%s  [%d] Key: %s -> Child: %d", indent(depth), i, string(cell.Key), cell.Child)
			dumpTree(t, btree, cell.Child, depth+1)
		}
		if page.RightPtr() != 0 {
			t.Logf("%s  [RIGHT] -> Child: %d", indent(depth), page.RightPtr())
			dumpTree(t, btree, page.RightPtr(), depth+1)
		}
	}
}

func indent(depth int) string {
	s := ""
	for i := 0; i < depth; i++ {
		s += "  "
	}
	return s
}
