package btree

import (
	"bytes"
	"errors"
)

// SplitResult represents the result of a page split
type SplitResult struct {
	SplitKey   []byte // Key to insert into parent
	NewPageID  uint32 // ID of the newly created page
	LeftPageID uint32 // ID of the original (left) page
}

// splitLeaf splits a full leaf page into two pages
// Returns the separator key and the new page ID
func (b *BTree) splitLeaf(page *Page, key, value []byte) (*SplitResult, error) {
	// Collect all cells including the new one
	numCells := page.NumCells()
	cells := make([]*Cell, 0, numCells+1)

	// Collect existing cells
	for i := uint16(0); i < numCells; i++ {
		cell, err := page.CellAt(i)
		if err != nil {
			return nil, err
		}
		cells = append(cells, CopyCell(cell))
	}

	// Insert new cell in sorted position
	newCell := &Cell{Key: key, Value: value}
	insertPos := 0
	for i, cell := range cells {
		if bytes.Compare(key, cell.Key) < 0 {
			insertPos = i
			break
		}
		insertPos = i + 1
	}

	// Insert new cell
	cells = append(cells[:insertPos], append([]*Cell{newCell}, cells[insertPos:]...)...)

	// Calculate split point (divide evenly)
	midpoint := len(cells) / 2

	// Create new page for second half
	newPage, err := b.pager.NewPage(PageTypeLeaf)
	if err != nil {
		return nil, err
	}

	// Clear original page
	page.setNumCells(0)
	page.setFreePtr(PageSize)

	// Add first half to original page
	for i := 0; i < midpoint; i++ {
		if err := page.InsertCell(cells[i]); err != nil {
			return nil, err
		}
	}

	// Add second half to new page
	for i := midpoint; i < len(cells); i++ {
		if err := newPage.InsertCell(cells[i]); err != nil {
			return nil, err
		}
	}

	// Link leaf pages (for range scans)
	oldRightPtr := page.RightPtr()
	page.SetRightPtr(newPage.ID())
	newPage.SetRightPtr(oldRightPtr)

	// Mark both pages as dirty
	b.pager.MarkDirty(page.ID())
	b.pager.MarkDirty(newPage.ID())

	// Note: Bytes written are tracked in pager.writePage(), not here

	// Separator key is the first key of the new page
	separatorCell, err := newPage.CellAt(0)
	if err != nil {
		return nil, err
	}

	return &SplitResult{
		SplitKey:   separatorCell.Key,
		NewPageID:  newPage.ID(),
		LeftPageID: page.ID(),
	}, nil
}

// splitInternal splits a full internal page
func (b *BTree) splitInternal(page *Page, key []byte, childPageID uint32) (*SplitResult, error) {
	// Collect all cells including the new one
	numCells := page.NumCells()
	cells := make([]*Cell, 0, numCells+1)

	// Collect existing cells
	for i := uint16(0); i < numCells; i++ {
		cell, err := page.CellAt(i)
		if err != nil {
			return nil, err
		}
		cells = append(cells, CopyCell(cell))
	}

	// Insert new cell in sorted position
	newCell := &Cell{Key: key, Child: childPageID}
	insertPos := 0
	for i, cell := range cells {
		if bytes.Compare(key, cell.Key) < 0 {
			insertPos = i
			break
		}
		insertPos = i + 1
	}

	// Insert new cell
	cells = append(cells[:insertPos], append([]*Cell{newCell}, cells[insertPos:]...)...)

	// Calculate split point
	midpoint := len(cells) / 2

	// The middle key will be promoted to parent
	// Left page gets cells [0, midpoint-1]
	// Right page gets cells [midpoint+1, end]
	// Middle key goes to parent

	middleCell := cells[midpoint]

	// Create new page for right half
	newPage, err := b.pager.NewPage(PageTypeInternal)
	if err != nil {
		return nil, err
	}

	// Clear original page
	oldRightPtr := page.RightPtr()
	page.setNumCells(0)
	page.setFreePtr(PageSize)

	// Add left half to original page
	for i := 0; i < midpoint; i++ {
		if err := page.InsertCell(cells[i]); err != nil {
			return nil, err
		}
	}

	// Set original page's right pointer to middle cell's child
	page.SetRightPtr(middleCell.Child)

	// Add right half to new page
	for i := midpoint + 1; i < len(cells); i++ {
		if err := newPage.InsertCell(cells[i]); err != nil {
			return nil, err
		}
	}

	// Set new page's right pointer
	newPage.SetRightPtr(oldRightPtr)

	// Mark both pages as dirty
	b.pager.MarkDirty(page.ID())
	b.pager.MarkDirty(newPage.ID())

	// Note: Bytes written are tracked in pager.writePage(), not here

	// Return middle key to be inserted into parent
	return &SplitResult{
		SplitKey:   middleCell.Key,
		NewPageID:  newPage.ID(),
		LeftPageID: page.ID(),
	}, nil
}

// insertAndSplit handles insertion with split if necessary
// This replaces the simple insertIntoLeaf/insertIntoInternal in btree.go
func (b *BTree) insertAndSplit(pageID uint32, key, value []byte) (bool, []byte, uint32, error) {
	page, err := b.pager.GetPage(pageID)
	if err != nil {
		return false, nil, 0, err
	}

	if page.IsLeaf() {
		// Try simple insert first
		cell := &Cell{Key: key, Value: value}
		err := page.InsertCell(cell)

		if err == nil {
			// Success, no split needed
			b.pager.MarkDirty(page.ID())
			// Note: Bytes written are tracked in pager.writePage(), not here
			return false, nil, 0, nil
		}

		if !errors.Is(err, ErrPageFull) {
			return false, nil, 0, err
		}

		// Page is full, split it
		result, err := b.splitLeaf(page, key, value)
		if err != nil {
			return false, nil, 0, err
		}

		return true, result.SplitKey, result.NewPageID, nil
	}

	// Internal node - find child to recurse into
	childPageID, err := GetChildPageID(page, key)
	if err != nil {
		return false, nil, 0, err
	}

	// Recursively insert into child
	splitOccurred, splitKey, newPageID, err := b.insertAndSplit(childPageID, key, value)
	if err != nil {
		return false, nil, 0, err
	}

	if !splitOccurred {
		// No split in child, we're done
		return false, nil, 0, nil
	}

	cell := &Cell{Key: splitKey, Child: newPageID}
	err = page.InsertCell(cell)

	if err == nil {
		// Successfully inserted separator, no split needed at this level
		b.pager.MarkDirty(page.ID())
		return false, nil, 0, nil
	}

	if !errors.Is(err, ErrPageFull) {
		return false, nil, 0, err
	}

	// This internal node is also full, split it
	result, err := b.splitInternal(page, splitKey, newPageID)
	if err != nil {
		return false, nil, 0, err
	}

	return true, result.SplitKey, result.NewPageID, nil
}

// handleRootSplit creates a new root when the root splits
func (b *BTree) handleRootSplit(oldRootID uint32, splitKey []byte, newPageID uint32) error {
	// Create new root page
	newRoot, err := b.pager.NewPage(PageTypeInternal)
	if err != nil {
		return err
	}

	cell := &Cell{
		Key:   splitKey,
		Child: newPageID,
	}

	if err := newRoot.InsertCell(cell); err != nil {
		return err
	}

	// Set right pointer to old root (LEFT page, keys < splitKey)
	newRoot.SetRightPtr(oldRootID)

	b.pager.MarkDirty(newRoot.ID())

	// Update root page ID in metadata
	if err := b.pager.SetRootPageID(newRoot.ID()); err != nil {
		return err
	}

	return nil
}
