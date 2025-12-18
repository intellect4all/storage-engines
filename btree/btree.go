package btree

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/intellect4all/storage-engines/common"
)

// Config holds configuration for the B-tree
type Config struct {
	DataDir   string
	Order     int // Max keys per page (fanout)
	CacheSize int // Number of pages to keep in memory
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig(dataDir string) Config {
	return Config{
		DataDir:   dataDir + "/btree.db",
		Order:     128,   // Good balance for 4KB pages
		CacheSize: 50000, // Cache 50,000 pages (~200MB memory)
		// Note: Larger cache reduces write amplification by minimizing page evictions.
		// Production databases typically use 128MB-2GB caches. For workloads where
		// the working set exceeds cache size, expect higher write amplification due
		// to page evictions. The theoretical 2-3x write amp is achieved when most
		// pages stay in cache.
	}
}

// BTree represents a B-tree storage engine
type BTree struct {
	config       Config
	pager        *Pager
	wal          *WAL
	mu           sync.RWMutex // Global lock (used for structural changes)
	latchManager *LatchManager // Page-level locks (for concurrent operations)

	// Statistics (atomic for lock-free access)
	stats struct {
		numKeys          int64
		writeCount       atomic.Int64
		readCount        atomic.Int64
		bytesWritten     atomic.Int64
		userBytesWritten atomic.Int64
	}

	closed atomic.Bool
}

// New creates or opens a B-tree database
func New(config Config) (*BTree, error) {
	// Create pager
	pager, err := NewPager(config.DataDir, config.CacheSize)
	if err != nil {
		return nil, err
	}

	// Create WAL
	walPath := config.DataDir + ".wal"
	wal, err := NewWAL(walPath)
	if err != nil {
		pager.Close()
		return nil, err
	}

	btree := &BTree{
		config:       config,
		pager:        pager,
		wal:          wal,
		latchManager: NewLatchManager(),
	}

	// Set WAL in pager so it can log page modifications
	pager.SetWAL(wal)

	// Perform WAL recovery if needed
	if err := btree.recoverFromWAL(); err != nil {
		pager.Close()
		wal.Close()
		return nil, err
	}

	return btree, nil
}

// recoverFromWAL replays WAL records to restore consistency
func (b *BTree) recoverFromWAL() error {
	records, err := b.wal.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read WAL: %w", err)
	}

	if len(records) == 0 {
		// No recovery needed
		return nil
	}

	// Replay each record
	for _, record := range records {
		switch record.Type {
		case WALRecordPageWrite:
			// Apply page modification
			// Note: Temporarily disable WAL logging during recovery
			// to avoid re-logging recovered operations
			oldWAL := b.pager.wal
			b.pager.wal = nil

			page, err := b.pager.GetPage(record.PageID)
			if err != nil {
				// Page doesn't exist on disk, this is expected during recovery
				// from a crash where new pages were created but not flushed
				// Create a blank page and apply the WAL record to it
				page = NewPage(record.PageID, PageTypeLeaf) // Will be overwritten by WAL data
				b.pager.cache[record.PageID] = page
			}

			// Apply the modification
			if record.Offset+record.Length <= PageSize {
				copy(page.data[record.Offset:record.Offset+record.Length], record.Data)
				page.SetDirty(true)
				b.pager.dirty[record.PageID] = true
			}

			// Restore WAL
			b.pager.wal = oldWAL

		case WALRecordCheckpoint:
			// Checkpoint reached, we can stop
			break
		}
	}

	// Update metadata to reflect recovered state
	// Find the highest page ID in cache to update NumPages
	maxPageID := b.pager.metadata.NumPages
	for pageID := range b.pager.cache {
		if pageID > maxPageID {
			maxPageID = pageID
		}
	}
	b.pager.metadata.NumPages = maxPageID + 1

	// Flush all recovered pages
	if err := b.pager.Sync(); err != nil {
		return fmt.Errorf("failed to flush recovered pages: %w", err)
	}

	// Truncate WAL after successful recovery
	if err := b.wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	return nil
}

// Put inserts or updates a key-value pair
func (b *BTree) Put(key, value []byte) error {
	if len(key) == 0 {
		return common.ErrKeyEmpty
	}

	if b.closed.Load() {
		return common.ErrClosed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Track user bytes written
	b.stats.userBytesWritten.Add(int64(len(key) + len(value)))
	b.stats.writeCount.Add(1)

	// TODO: Write to WAL (Phase 4)

	// Traverse tree to find leaf and insert with split handling
	rootPageID := b.pager.RootPageID()

	splitOccurred, splitKey, newPageID, err := b.insertAndSplit(rootPageID, key, value)
	if err != nil {
		return err
	}

	// Handle root split
	if splitOccurred {
		if err := b.handleRootSplit(rootPageID, splitKey, newPageID); err != nil {
			return err
		}
	}

	b.stats.numKeys++
	return nil
}


// findChild finds the child page ID for a given key in an internal node
// Cell semantics: Cell(K, P) means P contains keys >= K
func (b *BTree) findChild(page *Page, key []byte) uint32 {
	numCells := page.NumCells()

	// Find the first cell where key >= cell.Key
	// That cell's child contains the key
	for i := uint16(0); i < numCells; i++ {
		cell, err := page.CellAt(i)
		if err != nil {
			continue
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
			return cell.Child
		}
	}

	// Key < all cell keys, use right pointer (for keys less than minimum)
	return page.RightPtr()
}

// Get retrieves the value for a key
func (b *BTree) Get(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, common.ErrKeyEmpty
	}

	if b.closed.Load() {
		return nil, common.ErrClosed
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	b.stats.readCount.Add(1)

	// Start at root and traverse down
	pageID := b.pager.RootPageID()

	for {
		page, err := b.pager.GetPage(pageID)
		if err != nil {
			return nil, err
		}

		if page.IsLeaf() {
			// Search in leaf
			return b.searchLeaf(page, key)
		}

		// Internal node - find child
		pageID = b.findChild(page, key)
	}
}

// searchLeaf searches for a key in a leaf page
func (b *BTree) searchLeaf(page *Page, key []byte) ([]byte, error) {
	index := page.searchCell(key)
	if index < 0 {
		// Found
		index = -index - 1
		cell, err := page.CellAt(uint16(index))
		if err != nil {
			return nil, err
		}
		return cell.Value, nil
	}

	// Not found
	return nil, common.ErrKeyNotFound
}

// Delete removes a key from the tree
func (b *BTree) Delete(key []byte) error {
	if len(key) == 0 {
		return common.ErrKeyEmpty
	}

	if b.closed.Load() {
		return common.ErrClosed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// TODO: Write tombstone to WAL (Phase 4)

	// Find the key in leaf
	pageID := b.pager.RootPageID()

	for {
		page, err := b.pager.GetPage(pageID)
		if err != nil {
			return err
		}

		if page.IsLeaf() {
			// Delete from leaf
			return b.deleteFromLeaf(page, key)
		}

		// Internal node - find child
		pageID = b.findChild(page, key)
	}
}

// deleteFromLeaf deletes a key from a leaf page
func (b *BTree) deleteFromLeaf(page *Page, key []byte) error {
	index := page.searchCell(key)
	if index >= 0 {
		// Not found
		return common.ErrKeyNotFound
	}

	// Found
	index = -index - 1
	err := page.DeleteCell(uint16(index))
	if err != nil {
		return err
	}

	b.pager.MarkDirty(page.ID())
	b.stats.numKeys--

	// Check if page is underfull and needs rebalancing
	merged, err := b.mergeOrRedistribute(page.ID(), key)
	if err != nil {
		// Log error but don't fail the delete
		// Merge is an optimization, not critical for correctness
	}
	_ = merged // Suppress unused variable warning

	return nil
}

// Close flushes all dirty pages and closes the database
func (b *BTree) Close() error {
	if b.closed.Swap(true) {
		return nil // Already closed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Sync WAL
	if err := b.wal.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Flush all pages
	if err := b.pager.Sync(); err != nil {
		return fmt.Errorf("failed to sync pager: %w", err)
	}

	// Write checkpoint
	if err := b.wal.LogCheckpoint(); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	// Close WAL
	if err := b.wal.Close(); err != nil {
		return fmt.Errorf("failed to close WAL: %w", err)
	}

	return b.pager.Close()
}

// Sync flushes all dirty pages to disk
func (b *BTree) Sync() error {
	if b.closed.Load() {
		return common.ErrClosed
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Sync WAL first (write-ahead!)
	if err := b.wal.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Then sync pages
	if err := b.pager.Sync(); err != nil {
		return fmt.Errorf("failed to sync pager: %w", err)
	}

	// Write checkpoint after successful flush
	if err := b.wal.LogCheckpoint(); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	// Sync checkpoint to disk
	if err := b.wal.Sync(); err != nil {
		return fmt.Errorf("failed to sync checkpoint: %w", err)
	}

	// Truncate WAL after successful checkpoint
	// This is safe because all pages are now on disk
	if err := b.wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	return nil
}

// Stats returns statistics about the B-tree
func (b *BTree) Stats() common.Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	numPages := int(b.pager.NumPages())
	totalDiskSize := int64(numPages * PageSize)

	// Calculate logical data size from actual user bytes written
	logicalSize := b.stats.userBytesWritten.Load()
	if logicalSize == 0 {
		logicalSize = 1 // Avoid division by zero
	}

	// Get pager stats for accurate write amplification
	b.pager.mu.RLock()
	pagerBytesWritten := b.pager.stats.bytesWritten
	b.pager.mu.RUnlock()

	// Calculate write amplification: disk bytes written / user data bytes
	// Note: WAL is NOT yet implemented (see TODO in Put()), so no WAL overhead
	writeAmp := 1.0
	userBytes := b.stats.userBytesWritten.Load()
	if userBytes > 0 {
		writeAmp = float64(pagerBytesWritten) / float64(userBytes)
	}

	// Space amplification: disk space used / actual user data
	spaceAmp := float64(totalDiskSize) / float64(logicalSize)

	return common.Stats{
		NumKeys:       b.stats.numKeys,
		NumSegments:   numPages, // "Segments" = pages for B-tree
		TotalDiskSize: totalDiskSize,
		WriteCount:    b.stats.writeCount.Load(),
		ReadCount:     b.stats.readCount.Load(),
		WriteAmp:      writeAmp,
		SpaceAmp:      spaceAmp,
		// Note: cacheHitRate is not in common.Stats, but could be added for debugging
	}
}

// Compact is a no-op for B-tree (in-place updates mean no compaction needed!)
func (b *BTree) Compact() error {
	// B-tree doesn't need compaction
	return nil
}
