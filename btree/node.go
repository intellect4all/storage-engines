package btree

import (
	"bytes"
)

// NodeHelper provides helper functions for B-tree node operations

// SearchKey searches for a key in a page and returns the index
// For leaf pages: returns the index of the key if found, or insertion point if not
// For internal pages: returns the child page ID to follow
func SearchKey(page *Page, key []byte) int {
	return page.searchCell(key)
}

// GetChildPageID returns the child page ID for the given key in an internal node
// Cell semantics: Cell(K, P) means P contains keys >= K
// RightPtr contains keys < first cell's key
func GetChildPageID(page *Page, key []byte) (uint32, error) {
	if page.IsLeaf() {
		return 0, ErrCellNotFound
	}

	numCells := page.NumCells()

	// Find the last cell where key >= cell.Key
	// That cell's child contains the key
	for i := uint16(0); i < numCells; i++ {
		cell, err := page.CellAt(i)
		if err != nil {
			return 0, err
		}

		// If key >= cell.Key, check if this is the right cell
		// We want the LAST cell where key >= cell.Key
		if bytes.Compare(key, cell.Key) >= 0 {
			// Check if there's a next cell
			if i+1 < numCells {
				nextCell, err := page.CellAt(i + 1)
				if err == nil && bytes.Compare(key, nextCell.Key) >= 0 {
					// key also >= next cell, continue searching
					continue
				}
			}
			// This is the right cell
			return cell.Child, nil
		}
	}

	// Key < all cell keys, use right pointer (for keys less than minimum)
	rightPtr := page.RightPtr()
	if rightPtr == 0 {
		return 0, ErrCellNotFound
	}

	return rightPtr, nil
}

// InsertIntoLeaf inserts a key-value pair into a leaf node
func InsertIntoLeaf(page *Page, key, value []byte) error {
	if !page.IsLeaf() {
		return ErrCellNotFound
	}

	cell := &Cell{
		Key:   key,
		Value: value,
	}

	return page.InsertCell(cell)
}

// InsertIntoInternal inserts a separator key and child pointer into an internal node
func InsertIntoInternal(page *Page, key []byte, childPageID uint32) error {
	if page.IsLeaf() {
		return ErrCellNotFound
	}

	cell := &Cell{
		Key:   key,
		Child: childPageID,
	}

	return page.InsertCell(cell)
}

// GetMinKey returns the smallest key in a subtree rooted at the given page
func GetMinKey(pager *Pager, pageID uint32) ([]byte, error) {
	page, err := pager.GetPage(pageID)
	if err != nil {
		return nil, err
	}

	// Follow leftmost path to leaf
	for !page.IsLeaf() {
		// Get first child (leftmost)
		if page.NumCells() == 0 {
			return nil, ErrCellNotFound
		}

		cell, err := page.CellAt(0)
		if err != nil {
			return nil, err
		}

		page, err = pager.GetPage(cell.Child)
		if err != nil {
			return nil, err
		}
	}

	// Now at leaf, return first key
	if page.NumCells() == 0 {
		return nil, ErrCellNotFound
	}

	cell, err := page.CellAt(0)
	if err != nil {
		return nil, err
	}

	return cell.Key, nil
}

// GetMaxKey returns the largest key in a subtree rooted at the given page
func GetMaxKey(pager *Pager, pageID uint32) ([]byte, error) {
	page, err := pager.GetPage(pageID)
	if err != nil {
		return nil, err
	}

	// Follow rightmost path to leaf
	for !page.IsLeaf() {
		// Use right pointer
		rightPtr := page.RightPtr()
		if rightPtr == 0 {
			// No right pointer, use last cell's child
			numCells := page.NumCells()
			if numCells == 0 {
				return nil, ErrCellNotFound
			}

			cell, err := page.CellAt(numCells - 1)
			if err != nil {
				return nil, err
			}

			page, err = pager.GetPage(cell.Child)
			if err != nil {
				return nil, err
			}
		} else {
			page, err = pager.GetPage(rightPtr)
			if err != nil {
				return nil, err
			}
		}
	}

	// Now at leaf, return last key
	numCells := page.NumCells()
	if numCells == 0 {
		return nil, ErrCellNotFound
	}

	cell, err := page.CellAt(numCells - 1)
	if err != nil {
		return nil, err
	}

	return cell.Key, nil
}

// CopyCell creates a copy of a cell
func CopyCell(cell *Cell) *Cell {
	newCell := &Cell{
		Key:   make([]byte, len(cell.Key)),
		Child: cell.Child,
	}
	copy(newCell.Key, cell.Key)

	if cell.Value != nil {
		newCell.Value = make([]byte, len(cell.Value))
		copy(newCell.Value, cell.Value)
	}

	return newCell
}
