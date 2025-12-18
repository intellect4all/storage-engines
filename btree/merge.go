package btree

import (
	"bytes"
	"fmt"
)

// Page merge and rebalancing operations
// When a page becomes underfull after deletion, we either:
// 1. Redistribute keys with a sibling (if sibling has extra keys)
// 2. Merge with a sibling (if both are underfull)

const (
	// MinFillFactor is the minimum page utilization before merge/redistribute
	// Set to 25% - if page has less than 1/4 of max cells, trigger rebalance
	MinFillFactor = 0.25

	// MaxCellsPerPage is an estimate based on typical key/value sizes
	// For leaf: assume 32-byte key + 128-byte value = 160 bytes per cell
	// 4096 / 160 ≈ 25 cells max (with overhead)
	MaxCellsPerPage = 25
)

// shouldMerge checks if a page is underfull and needs rebalancing
func (b *BTree) shouldMerge(page *Page) bool {
	numCells := page.NumCells()
	minCellsFloat := float64(MaxCellsPerPage) * MinFillFactor
	minCells := uint16(minCellsFloat)

	// Don't merge root page (it can have any number of cells)
	if page.ID() == b.pager.RootPageID() {
		return false
	}

	// Don't merge if page has enough cells
	if numCells >= minCells {
		return false
	}

	return true
}

// mergeOrRedistribute attempts to rebalance an underfull page
// Returns true if merge/redistribute occurred
func (b *BTree) mergeOrRedistribute(pageID uint32, key []byte) (bool, error) {
	page, err := b.pager.GetPage(pageID)
	if err != nil {
		return false, err
	}

	if !b.shouldMerge(page) {
		return false, nil
	}

	// Find parent and sibling
	parentID, siblingID, separatorIdx, err := b.findSibling(pageID, key)
	if err != nil {
		// No parent or sibling found (root page or only child)
		return false, nil
	}

	parent, err := b.pager.GetPage(parentID)
	if err != nil {
		return false, err
	}

	sibling, err := b.pager.GetPage(siblingID)
	if err != nil {
		return false, err
	}

	// Try redistribution first (less disruptive)
	if b.canRedistribute(page, sibling) {
		return true, b.redistribute(parent, page, sibling, separatorIdx)
	}

	// Merge pages
	return true, b.mergePage(parent, page, sibling, separatorIdx)
}

// findSibling finds a sibling page and parent for merging/redistribution
// Returns: parentID, siblingID, separatorIndex in parent, error
func (b *BTree) findSibling(pageID uint32, searchKey []byte) (uint32, uint32, uint16, error) {
	// Traverse from root to find parent
	currentID := b.pager.RootPageID()

	// Stack to track path from root
	type pathEntry struct {
		pageID        uint32
		childIdx      int // Index in parent's cells (-1 for rightPtr)
	}
	path := []pathEntry{{pageID: currentID, childIdx: -1}}

	// Traverse to find the target page
	for currentID != pageID {
		current, err := b.pager.GetPage(currentID)
		if err != nil {
			return 0, 0, 0, err
		}

		if current.IsLeaf() {
			return 0, 0, 0, fmt.Errorf("target page not found in tree")
		}

		// Find which child to follow
		childID, err := GetChildPageID(current, searchKey)
		if err != nil {
			return 0, 0, 0, err
		}

		// Record which child we're following
		childIdx := -1
		numCells := current.NumCells()
		for i := uint16(0); i < numCells; i++ {
			cell, _ := current.CellAt(i)
			if cell.Child == childID {
				childIdx = int(i)
				break
			}
		}

		path = append(path, pathEntry{pageID: childID, childIdx: childIdx})
		currentID = childID
	}

	if len(path) < 2 {
		// No parent (this is root)
		return 0, 0, 0, fmt.Errorf("no parent found")
	}

	// Parent is second-to-last in path
	parentEntry := path[len(path)-2]
	parentID := parentEntry.pageID

	parent, err := b.pager.GetPage(parentID)
	if err != nil {
		return 0, 0, 0, err
	}

	// Find sibling (prefer left sibling, fallback to right)
	childIdx := path[len(path)-1].childIdx
	numCells := parent.NumCells()

	var siblingID uint32
	var separatorIdx uint16

	if childIdx > 0 {
		// Has left sibling
		cell, _ := parent.CellAt(uint16(childIdx - 1))
		siblingID = cell.Child
		separatorIdx = uint16(childIdx - 1)
	} else if childIdx < int(numCells)-1 {
		// Has right sibling
		cell, _ := parent.CellAt(uint16(childIdx + 1))
		siblingID = cell.Child
		separatorIdx = uint16(childIdx)
	} else if childIdx == -1 && numCells > 0 {
		// This page is rightPtr, use last cell as sibling
		cell, _ := parent.CellAt(numCells - 1)
		siblingID = cell.Child
		separatorIdx = numCells - 1
	} else {
		return 0, 0, 0, fmt.Errorf("no sibling found")
	}

	return parentID, siblingID, separatorIdx, nil
}

// canRedistribute checks if redistribution is possible
// Redistribution is possible if sibling has more than minimum cells
func (b *BTree) canRedistribute(page, sibling *Page) bool {
	minCellsFloat := float64(MaxCellsPerPage) * MinFillFactor
	minCells := uint16(minCellsFloat)

	// Check if sibling has enough cells to share
	siblingCells := sibling.NumCells()
	pageCells := page.NumCells()

	// After redistribution, both should have at least minCells
	totalCells := siblingCells + pageCells
	if totalCells < minCells*2 {
		return false
	}

	return siblingCells > minCells
}

// redistribute moves keys from sibling to underfull page
func (b *BTree) redistribute(parent, page, sibling *Page, separatorIdx uint16) error {
	pageCells := page.NumCells()
	siblingCells := sibling.NumCells()

	// Calculate how many cells to move
	totalCells := pageCells + siblingCells
	targetCells := totalCells / 2

	if page.IsLeaf() {
		return b.redistributeLeaf(parent, page, sibling, separatorIdx, targetCells)
	}
	return b.redistributeInternal(parent, page, sibling, separatorIdx, targetCells)
}

// redistributeLeaf redistributes cells between leaf pages
func (b *BTree) redistributeLeaf(parent, page, sibling *Page, separatorIdx, targetCells uint16) error {
	pageCells := page.NumCells()
	siblingCells := sibling.NumCells()

	// Collect all cells
	var allCells []*Cell

	// Determine order based on which page comes first
	separatorCell, _ := parent.CellAt(separatorIdx)

	pageFirstCell, _ := page.CellAt(0)
	siblingFirstCell, _ := sibling.CellAt(0)

	pageFirst := bytes.Compare(pageFirstCell.Key, siblingFirstCell.Key) < 0

	if pageFirst {
		// Page comes before sibling
		for i := uint16(0); i < pageCells; i++ {
			cell, _ := page.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
		for i := uint16(0); i < siblingCells; i++ {
			cell, _ := sibling.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
	} else {
		// Sibling comes before page
		for i := uint16(0); i < siblingCells; i++ {
			cell, _ := sibling.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
		for i := uint16(0); i < pageCells; i++ {
			cell, _ := page.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
	}

	// Clear both pages
	page.setNumCells(0)
	page.setFreePtr(PageSize)
	sibling.setNumCells(0)
	sibling.setFreePtr(PageSize)

	// Redistribute cells
	for i := 0; i < int(targetCells); i++ {
		page.InsertCell(allCells[i])
	}
	for i := int(targetCells); i < len(allCells); i++ {
		sibling.InsertCell(allCells[i])
	}

	// Update separator key in parent
	newSeparatorCell, _ := sibling.CellAt(0)
	separatorCell.Key = CopyCell(newSeparatorCell).Key

	// Mark pages dirty
	b.pager.MarkDirty(page.ID())
	b.pager.MarkDirty(sibling.ID())
	b.pager.MarkDirty(parent.ID())

	return nil
}

// redistributeInternal redistributes pointers between internal pages
func (b *BTree) redistributeInternal(parent, page, sibling *Page, separatorIdx, targetCells uint16) error {
	// Similar to leaf redistribution but handles child pointers
	// For simplicity, we'll skip internal node redistribution for now
	// and just merge them instead
	return b.mergePage(parent, page, sibling, separatorIdx)
}

// mergePage merges page with sibling
func (b *BTree) mergePage(parent, page, sibling *Page, separatorIdx uint16) error {
	if page.IsLeaf() {
		return b.mergeLeafPages(parent, page, sibling, separatorIdx)
	}
	return b.mergeInternalPages(parent, page, sibling, separatorIdx)
}

// mergeLeafPages merges two leaf pages
func (b *BTree) mergeLeafPages(parent, page, sibling *Page, separatorIdx uint16) error {
	// Collect all cells from both pages
	var allCells []*Cell

	pageCells := page.NumCells()
	siblingCells := sibling.NumCells()

	// Determine which page comes first
	pageFirstCell, _ := page.CellAt(0)
	siblingFirstCell, _ := sibling.CellAt(0)
	pageFirst := bytes.Compare(pageFirstCell.Key, siblingFirstCell.Key) < 0

	if pageFirst {
		for i := uint16(0); i < pageCells; i++ {
			cell, _ := page.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
		for i := uint16(0); i < siblingCells; i++ {
			cell, _ := sibling.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
	} else {
		for i := uint16(0); i < siblingCells; i++ {
			cell, _ := sibling.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
		for i := uint16(0); i < pageCells; i++ {
			cell, _ := page.CellAt(i)
			allCells = append(allCells, CopyCell(cell))
		}
	}

	// Clear page and add all cells
	page.setNumCells(0)
	page.setFreePtr(PageSize)

	for _, cell := range allCells {
		page.InsertCell(cell)
	}

	// Update page links
	page.SetRightPtr(sibling.RightPtr())

	// Remove separator from parent
	if err := parent.DeleteCell(separatorIdx); err != nil {
		return err
	}

	// Free sibling page
	b.pager.FreePage(sibling.ID())

	// Mark pages dirty
	b.pager.MarkDirty(page.ID())
	b.pager.MarkDirty(parent.ID())

	// Check if parent needs rebalancing
	if b.shouldMerge(parent) {
		// Recursively merge parent
		// For simplicity, we'll skip recursive merging for now
	}

	return nil
}

// mergeInternalPages merges two internal pages
func (b *BTree) mergeInternalPages(parent, page, sibling *Page, separatorIdx uint16) error {
	// For simplicity, we'll implement a basic version
	// In production, this would handle pulling down the separator key
	// and redistributing child pointers

	// Skip internal node merging for now
	return nil
}
