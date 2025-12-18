package btree

import (
	"sync"

	"github.com/intellect4all/storage-engines/common"
)

// Latch represents a page-level lock
// B-tree uses "latch coupling" (also called "lock coupling") to allow
// concurrent tree traversals:
// 1. Lock parent
// 2. Lock child
// 3. Unlock parent (if child won't split/merge)
// 4. Continue down the tree
//
// This allows multiple threads to traverse different paths concurrently

type LatchMode int

const (
	LatchRead  LatchMode = iota // Shared lock (multiple readers)
	LatchWrite                   // Exclusive lock (single writer)
)

// PageLatch represents a per-page read-write lock
type PageLatch struct {
	mu sync.RWMutex
}

// Lock acquires a latch in the specified mode
func (l *PageLatch) Lock(mode LatchMode) {
	if mode == LatchRead {
		l.mu.RLock()
	} else {
		l.mu.Lock()
	}
}

// Unlock releases the latch
func (l *PageLatch) Unlock(mode LatchMode) {
	if mode == LatchRead {
		l.mu.RUnlock()
	} else {
		l.mu.Unlock()
	}
}

// TryLock attempts to acquire the latch without blocking
func (l *PageLatch) TryLock(mode LatchMode) bool {
	if mode == LatchRead {
		return l.mu.TryRLock()
	}
	return l.mu.TryLock()
}

// LatchManager manages page-level latches
type LatchManager struct {
	latches map[uint32]*PageLatch
	mu      sync.Mutex // Protects the latches map
}

// NewLatchManager creates a new latch manager
func NewLatchManager() *LatchManager {
	return &LatchManager{
		latches: make(map[uint32]*PageLatch),
	}
}

// GetLatch returns the latch for a page, creating it if necessary
func (lm *LatchManager) GetLatch(pageID uint32) *PageLatch {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	latch, exists := lm.latches[pageID]
	if !exists {
		latch = &PageLatch{}
		lm.latches[pageID] = latch
	}

	return latch
}

// LatchCoupling implements the latch coupling protocol for tree traversal
type LatchCoupling struct {
	lm           *LatchManager
	heldLatches  []uint32
	heldModes    []LatchMode
}

// NewLatchCoupling creates a new latch coupling context
func NewLatchCoupling(lm *LatchManager) *LatchCoupling {
	return &LatchCoupling{
		lm:          lm,
		heldLatches: make([]uint32, 0, 4), // Typical tree height
		heldModes:   make([]LatchMode, 0, 4),
	}
}

// AcquireLatch acquires a latch and tracks it
func (lc *LatchCoupling) AcquireLatch(pageID uint32, mode LatchMode) {
	latch := lc.lm.GetLatch(pageID)
	latch.Lock(mode)

	lc.heldLatches = append(lc.heldLatches, pageID)
	lc.heldModes = append(lc.heldModes, mode)
}

// ReleaseParent releases all latches except the most recent one
// This is the "coupling" part - we keep the child latched while releasing the parent
func (lc *LatchCoupling) ReleaseParent() {
	if len(lc.heldLatches) < 2 {
		return
	}

	// Release all but the last (current) latch
	for i := 0; i < len(lc.heldLatches)-1; i++ {
		pageID := lc.heldLatches[i]
		mode := lc.heldModes[i]

		latch := lc.lm.GetLatch(pageID)
		latch.Unlock(mode)
	}

	// Keep only the current latch
	if len(lc.heldLatches) > 0 {
		lastIdx := len(lc.heldLatches) - 1
		lc.heldLatches = []uint32{lc.heldLatches[lastIdx]}
		lc.heldModes = []LatchMode{lc.heldModes[lastIdx]}
	}
}

// ReleaseAll releases all held latches
func (lc *LatchCoupling) ReleaseAll() {
	for i := len(lc.heldLatches) - 1; i >= 0; i-- {
		pageID := lc.heldLatches[i]
		mode := lc.heldModes[i]

		latch := lc.lm.GetLatch(pageID)
		latch.Unlock(mode)
	}

	lc.heldLatches = lc.heldLatches[:0]
	lc.heldModes = lc.heldModes[:0]
}

// ConcurrentGet performs a Get operation with latch coupling
func (b *BTree) ConcurrentGet(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, common.ErrKeyEmpty
	}

	if b.closed.Load() {
		return nil, common.ErrClosed
	}

	// Use latch coupling instead of global lock
	lc := NewLatchCoupling(b.latchManager)
	defer lc.ReleaseAll()

	b.stats.readCount.Add(1)

	// Start at root
	pageID := b.pager.RootPageID()

	for {
		// Acquire read latch on current page
		lc.AcquireLatch(pageID, LatchRead)

		page, err := b.pager.GetPage(pageID)
		if err != nil {
			return nil, err
		}

		if page.IsLeaf() {
			// Found leaf, search in it
			value, err := b.searchLeaf(page, key)

			// Release all latches
			lc.ReleaseAll()

			return value, err
		}

		// Internal node - find child
		childPageID := b.findChild(page, key)

		// Release parent latch (we now have child)
		// This is safe because we're doing a read and the tree structure
		// can't change under us (writes use exclusive latches)
		lc.ReleaseParent()

		pageID = childPageID
	}
}

// ConcurrentPut performs a Put operation with latch coupling
func (b *BTree) ConcurrentPut(key, value []byte) error {
	if len(key) == 0 {
		return common.ErrKeyEmpty
	}

	if b.closed.Load() {
		return common.ErrClosed
	}

	// For writes, we need to be more careful about latch coupling
	// We acquire write latches when we might need to split

	// Use latch coupling
	lc := NewLatchCoupling(b.latchManager)
	defer lc.ReleaseAll()

	// Track user bytes written
	b.stats.userBytesWritten.Add(int64(len(key) + len(value)))
	b.stats.writeCount.Add(1)

	// Traverse tree to find leaf
	// For simplicity, we'll acquire exclusive latches all the way down
	// A more sophisticated implementation would use read latches until
	// we determine a split is needed

	rootPageID := b.pager.RootPageID()

	// Acquire write latch on root
	lc.AcquireLatch(rootPageID, LatchWrite)

	splitOccurred, splitKey, newPageID, err := b.insertAndSplit(rootPageID, key, value)
	if err != nil {
		return err
	}

	// Handle root split (requires updating metadata)
	if splitOccurred {
		// Need global lock for root split (modifies tree structure)
		b.mu.Lock()
		err := b.handleRootSplit(rootPageID, splitKey, newPageID)
		b.mu.Unlock()

		if err != nil {
			return err
		}
	}

	b.stats.numKeys++
	return nil
}
