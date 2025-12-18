package btree

import (
	"fmt"
	"testing"
)

func TestPageV2Format(t *testing.T) {
	// Create a V2 page (new format with varint)
	page := NewPage(1, PageTypeLeaf)

	// Verify version is set to V2
	if page.Version() != PageFormatV2 {
		t.Errorf("Expected page version %d, got %d", PageFormatV2, page.Version())
	}

	// Insert some cells with small keys (should benefit from varint)
	testCells := []*Cell{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	for _, cell := range testCells {
		if err := page.InsertCell(cell); err != nil {
			t.Fatalf("Failed to insert cell: %v", err)
		}
	}

	// Verify we can read them back
	if page.NumCells() != 3 {
		t.Errorf("Expected 3 cells, got %d", page.NumCells())
	}

	for i, expected := range testCells {
		cell, err := page.CellAt(uint16(i))
		if err != nil {
			t.Fatalf("Failed to read cell %d: %v", i, err)
		}

		if string(cell.Key) != string(expected.Key) {
			t.Errorf("Cell %d: key mismatch. Expected %s, got %s", i, expected.Key, cell.Key)
		}

		if string(cell.Value) != string(expected.Value) {
			t.Errorf("Cell %d: value mismatch. Expected %s, got %s", i, expected.Value, cell.Value)
		}
	}
}

func TestPageV1Compatibility(t *testing.T) {
	// Create a page and manually set it to V1 format
	page := NewPage(1, PageTypeLeaf)
	page.data[HeaderOffsetVersion] = PageFormatV1

	if page.Version() != PageFormatV1 {
		t.Errorf("Expected page version %d, got %d", PageFormatV1, page.Version())
	}

	// Insert cells (should use V1 encoding)
	testCells := []*Cell{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
	}

	for _, cell := range testCells {
		if err := page.InsertCell(cell); err != nil {
			t.Fatalf("Failed to insert cell: %v", err)
		}
	}

	// Verify we can read them back
	for i, expected := range testCells {
		cell, err := page.CellAt(uint16(i))
		if err != nil {
			t.Fatalf("Failed to read cell %d: %v", i, err)
		}

		if string(cell.Key) != string(expected.Key) {
			t.Errorf("Cell %d: key mismatch. Expected %s, got %s", i, expected.Key, cell.Key)
		}

		if string(cell.Value) != string(expected.Value) {
			t.Errorf("Cell %d: value mismatch. Expected %s, got %s", i, expected.Value, cell.Value)
		}
	}
}

func TestPageV2SpaceSavings(t *testing.T) {
	// Create two pages with same data, one V1 and one V2
	pageV1 := NewPage(1, PageTypeLeaf)
	pageV1.data[HeaderOffsetVersion] = PageFormatV1

	pageV2 := NewPage(2, PageTypeLeaf)
	// V2 is default

	// Insert same cells to both
	testCells := []*Cell{
		{Key: []byte("a"), Value: []byte("value1")},
		{Key: []byte("b"), Value: []byte("value2")},
		{Key: []byte("c"), Value: []byte("value3")},
		{Key: []byte("d"), Value: []byte("value4")},
		{Key: []byte("e"), Value: []byte("value5")},
	}

	for _, cell := range testCells {
		if err := pageV1.InsertCell(cell); err != nil {
			t.Fatalf("V1: Failed to insert cell: %v", err)
		}
		if err := pageV2.InsertCell(cell); err != nil {
			t.Fatalf("V2: Failed to insert cell: %v", err)
		}
	}

	// Calculate space used
	v1FreePtr := pageV1.freePtr()
	v2FreePtr := pageV2.freePtr()

	v1SpaceUsed := PageSize - int(v1FreePtr)
	v2SpaceUsed := PageSize - int(v2FreePtr)

	spaceSaved := v1SpaceUsed - v2SpaceUsed
	savingsPercent := float64(spaceSaved) / float64(v1SpaceUsed) * 100

	t.Logf("Space comparison for %d small cells:", len(testCells))
	t.Logf("  V1 space used: %d bytes", v1SpaceUsed)
	t.Logf("  V2 space used: %d bytes", v2SpaceUsed)
	t.Logf("  Space saved: %d bytes (%.2f%%)", spaceSaved, savingsPercent)

	// V2 should use less space for small keys
	if spaceSaved <= 0 {
		t.Errorf("Expected V2 to save space, but V1=%d V2=%d", v1SpaceUsed, v2SpaceUsed)
	}

	// With 5 cells, each saving 2 bytes of overhead, we should save ~10 bytes
	expectedSavings := len(testCells) * 2
	if spaceSaved < expectedSavings {
		t.Errorf("Expected at least %d bytes saved, got %d", expectedSavings, spaceSaved)
	}
}

func TestPageV2MoreKeys(t *testing.T) {
	// Test that V2 can fit more keys per page due to reduced overhead
	pageV1 := NewPage(1, PageTypeLeaf)
	pageV1.data[HeaderOffsetVersion] = PageFormatV1

	pageV2 := NewPage(2, PageTypeLeaf)

	// Insert keys until pages are full
	v1Count := 0
	v2Count := 0

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("k%02d", i))
		value := []byte(fmt.Sprintf("v%02d", i))

		// Try V1
		if !pageV1.IsFull(len(key), len(value)) {
			pageV1.InsertCell(&Cell{Key: key, Value: value})
			v1Count++
		}

		// Try V2
		if !pageV2.IsFull(len(key), len(value)) {
			pageV2.InsertCell(&Cell{Key: key, Value: value})
			v2Count++
		}

		if pageV1.IsFull(len(key), len(value)) && pageV2.IsFull(len(key), len(value)) {
			break
		}
	}

	improvement := float64(v2Count-v1Count) / float64(v1Count) * 100

	t.Logf("Keys per page:")
	t.Logf("  V1: %d keys", v1Count)
	t.Logf("  V2: %d keys", v2Count)
	t.Logf("  Improvement: %d more keys (%.2f%%)", v2Count-v1Count, improvement)

	// V2 should fit more keys
	if v2Count <= v1Count {
		t.Errorf("Expected V2 to fit more keys than V1, but V1=%d V2=%d", v1Count, v2Count)
	}
}

func TestInternalNodeV2(t *testing.T) {
	// Test V2 format with internal nodes
	page := NewPage(1, PageTypeInternal)

	if page.Version() != PageFormatV2 {
		t.Errorf("Expected page version %d, got %d", PageFormatV2, page.Version())
	}

	// Insert internal node cells
	testCells := []*Cell{
		{Key: []byte("key1"), Child: 10},
		{Key: []byte("key2"), Child: 20},
		{Key: []byte("key3"), Child: 30},
	}

	for _, cell := range testCells {
		if err := page.InsertCell(cell); err != nil {
			t.Fatalf("Failed to insert cell: %v", err)
		}
	}

	// Verify we can read them back
	for i, expected := range testCells {
		cell, err := page.CellAt(uint16(i))
		if err != nil {
			t.Fatalf("Failed to read cell %d: %v", i, err)
		}

		if string(cell.Key) != string(expected.Key) {
			t.Errorf("Cell %d: key mismatch. Expected %s, got %s", i, expected.Key, cell.Key)
		}

		if cell.Child != expected.Child {
			t.Errorf("Cell %d: child mismatch. Expected %d, got %d", i, expected.Child, cell.Child)
		}
	}
}

func BenchmarkPageInsertV1(b *testing.B) {
	page := NewPage(1, PageTypeLeaf)
	page.data[HeaderOffsetVersion] = PageFormatV1

	key := []byte("testkey")
	value := []byte("testvalue")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset page between runs
		if page.NumCells() > 100 {
			page = NewPage(1, PageTypeLeaf)
			page.data[HeaderOffsetVersion] = PageFormatV1
		}

		cell := &Cell{Key: key, Value: value}
		page.InsertCell(cell)
	}
}

func BenchmarkPageInsertV2(b *testing.B) {
	page := NewPage(1, PageTypeLeaf)

	key := []byte("testkey")
	value := []byte("testvalue")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset page between runs
		if page.NumCells() > 100 {
			page = NewPage(1, PageTypeLeaf)
		}

		cell := &Cell{Key: key, Value: value}
		page.InsertCell(cell)
	}
}
