package btree

import (
	"bytes"

	"github.com/intellect4all/storage-engines/common"
)

// Iterator implements range scanning over B-tree keys
type Iterator struct {
	btree       *BTree
	currentPage *Page
	cellIndex   uint16
	endKey      []byte
	err         error
	started     bool
	firstCall   bool // Track if this is the first Next() call
}

// NewIterator creates a new iterator for the given key range
func (b *BTree) NewIterator(startKey, endKey []byte) *Iterator {
	return &Iterator{
		btree:   b,
		endKey:  endKey,
		started: false,
	}
}

// Scan returns an iterator for the given key range
func (b *BTree) Scan(startKey, endKey []byte) (common.Iterator, error) {
	it := b.NewIterator(startKey, endKey)

	// Seek to start position
	if err := it.seek(startKey); err != nil {
		return nil, err
	}

	return it, nil
}

// seek positions the iterator at the first key >= startKey
func (it *Iterator) seek(startKey []byte) error {
	if len(startKey) == 0 {
		// Start from beginning - find leftmost leaf
		pageID := it.btree.pager.RootPageID()
		page, err := it.btree.pager.GetPage(pageID)
		if err != nil {
			return err
		}

		// Follow leftmost path to leaf
		for !page.IsLeaf() {
			if page.NumCells() == 0 {
				// Empty internal node
				it.currentPage = nil
				return nil
			}

			// Get first child (leftmost)
			cell, err := page.CellAt(0)
			if err != nil {
				return err
			}

			page, err = it.btree.pager.GetPage(cell.Child)
			if err != nil {
				return err
			}
		}

		it.currentPage = page
		it.cellIndex = 0
		it.started = true
		it.firstCall = true // First Next() should not advance
		return nil
	}

	// Traverse tree to find leaf containing startKey
	pageID := it.btree.pager.RootPageID()

	for {
		page, err := it.btree.pager.GetPage(pageID)
		if err != nil {
			it.err = err
			return err
		}

		if page.IsLeaf() {
			// Found leaf, binary search for start position
			it.currentPage = page
			index := page.searchCell(startKey)
			if index < 0 {
				// Found exact match
				it.cellIndex = uint16(-index - 1)
			} else {
				// Not found, index is insertion point (first key >= startKey)
				it.cellIndex = uint16(index)
			}
			it.started = true
			it.firstCall = true // First Next() should not advance
			return nil
		}

		// Internal node - find child
		childPageID, err := GetChildPageID(page, startKey)
		if err != nil {
			it.err = err
			return err
		}
		pageID = childPageID
	}
}

// Next advances the iterator and returns true if there's a valid key-value pair
func (it *Iterator) Next() bool {
	if it.err != nil {
		return false
	}

	if !it.started {
		it.err = common.ErrClosed
		return false
	}

	// Check if we're at the end of current page
	if it.currentPage == nil {
		return false
	}

	// If this is NOT the first call, advance to next cell
	if !it.firstCall {
		it.cellIndex++
	} else {
		it.firstCall = false // Clear flag after first call
	}

	// Check if current position is valid
	if it.cellIndex >= it.currentPage.NumCells() {
		// Move to next leaf page
		rightPtr := it.currentPage.RightPtr()
		if rightPtr == 0 {
			// End of tree
			it.currentPage = nil
			return false
		}

		// Load next page
		nextPage, err := it.btree.pager.GetPage(rightPtr)
		if err != nil {
			it.err = err
			return false
		}

		it.currentPage = nextPage
		it.cellIndex = 0
	}

	// Check if current key is beyond endKey
	if it.endKey != nil {
		cell, err := it.currentPage.CellAt(it.cellIndex)
		if err != nil {
			it.err = err
			return false
		}

		if bytes.Compare(cell.Key, it.endKey) >= 0 {
			// Beyond end key
			it.currentPage = nil
			return false
		}
	}

	// Current position is valid, return true
	return true
}

// Key returns the current key
func (it *Iterator) Key() []byte {
	if it.currentPage == nil {
		return nil
	}

	cell, err := it.currentPage.CellAt(it.cellIndex)
	if err != nil {
		it.err = err
		return nil
	}

	return cell.Key
}

// Value returns the current value
func (it *Iterator) Value() []byte {
	if it.currentPage == nil {
		return nil
	}

	cell, err := it.currentPage.CellAt(it.cellIndex)
	if err != nil {
		it.err = err
		return nil
	}

	// Note: cellIndex is advanced in Next(), not here
	return cell.Value
}

// Error returns any error encountered during iteration
func (it *Iterator) Error() error {
	return it.err
}

// Close closes the iterator
func (it *Iterator) Close() error {
	it.currentPage = nil
	return nil
}
