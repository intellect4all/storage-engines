package btree

import (
	"container/list"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
)

const (
	// Metadata page (page 0) layout
	MetadataPageID        = 0
	MetadataOffsetMagic   = 0  // 4 bytes
	MetadataOffsetRoot    = 4  // 4 bytes
	MetadataOffsetNumPage = 8  // 4 bytes
	MetadataOffsetFreeList = 12 // 4 bytes

	MetadataMagic = 0x42545245 // "BTRE" in hex
)

var (
	ErrInvalidDatabase = errors.New("invalid database file")
	ErrDatabaseClosed  = errors.New("database is closed")
)

// Metadata stores database metadata
type Metadata struct {
	Magic       uint32
	RootPageID  uint32
	NumPages    uint32
	FreeListPtr uint32
}

// Pager manages page I/O and caching
type Pager struct {
	file      *os.File
	mu        sync.RWMutex
	cache     map[uint32]*Page           // Page cache
	lru       *list.List                 // LRU list for eviction
	lruMap    map[uint32]*list.Element   // Quick lookup for LRU elements
	cacheSize int                        // Max pages in cache
	dirty     map[uint32]bool            // Track dirty pages
	metadata  *Metadata
	closed    bool
	wal       *WAL                       // Write-Ahead Log (optional)

	// Statistics
	stats struct {
		pageWrites    int64 // Number of page writes to disk
		pageReads     int64 // Number of page reads from disk
		cacheHits     int64 // Number of cache hits
		bytesWritten  int64 // Total bytes written to disk (pages)
	}
}

// lruEntry represents an entry in the LRU list
type lruEntry struct {
	pageID uint32
}

// NewPager creates a new pager
func NewPager(filename string, cacheSize int) (*Pager, error) {
	// Try to open existing file
	file, err := os.OpenFile(filename, os.O_RDWR, 0644)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// Create new file
		return createPager(filename, cacheSize)
	}

	// Load existing database
	return loadPager(file, cacheSize)
}

// createPager creates a new pager with a fresh database
func createPager(filename string, cacheSize int) (*Pager, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	pager := &Pager{
		file:      file,
		cache:     make(map[uint32]*Page),
		lru:       list.New(),
		lruMap:    make(map[uint32]*list.Element),
		cacheSize: cacheSize,
		dirty:     make(map[uint32]bool),
		metadata: &Metadata{
			Magic:       MetadataMagic,
			RootPageID:  1, // Root starts at page 1
			NumPages:    2, // Page 0 (metadata) + Page 1 (root)
			FreeListPtr: 0, // No free pages initially
		},
	}

	// Write metadata page
	if err := pager.writeMetadata(); err != nil {
		file.Close()
		os.Remove(filename)
		return nil, err
	}

	// Create initial root page (empty leaf)
	rootPage := NewPage(1, PageTypeLeaf)
	if err := pager.writePage(rootPage); err != nil {
		file.Close()
		os.Remove(filename)
		return nil, err
	}

	return pager, nil
}

// loadPager loads an existing database
func loadPager(file *os.File, cacheSize int) (*Pager, error) {
	pager := &Pager{
		file:      file,
		cache:     make(map[uint32]*Page),
		lru:       list.New(),
		lruMap:    make(map[uint32]*list.Element),
		cacheSize: cacheSize,
		dirty:     make(map[uint32]bool),
	}

	// Read metadata
	metadata, err := pager.readMetadata()
	if err != nil {
		file.Close()
		return nil, err
	}

	pager.metadata = metadata
	return pager, nil
}

// readMetadata reads the metadata from page 0
func (p *Pager) readMetadata() (*Metadata, error) {
	data := make([]byte, PageSize)
	n, err := p.file.ReadAt(data, 0)
	if err != nil {
		return nil, err
	}
	if n != PageSize {
		return nil, ErrInvalidDatabase
	}

	meta := &Metadata{
		Magic:       binary.BigEndian.Uint32(data[MetadataOffsetMagic:]),
		RootPageID:  binary.BigEndian.Uint32(data[MetadataOffsetRoot:]),
		NumPages:    binary.BigEndian.Uint32(data[MetadataOffsetNumPage:]),
		FreeListPtr: binary.BigEndian.Uint32(data[MetadataOffsetFreeList:]),
	}

	if meta.Magic != MetadataMagic {
		return nil, ErrInvalidDatabase
	}

	return meta, nil
}

// writeMetadata writes the metadata to page 0
func (p *Pager) writeMetadata() error {
	data := make([]byte, PageSize)
	binary.BigEndian.PutUint32(data[MetadataOffsetMagic:], p.metadata.Magic)
	binary.BigEndian.PutUint32(data[MetadataOffsetRoot:], p.metadata.RootPageID)
	binary.BigEndian.PutUint32(data[MetadataOffsetNumPage:], p.metadata.NumPages)
	binary.BigEndian.PutUint32(data[MetadataOffsetFreeList:], p.metadata.FreeListPtr)

	_, err := p.file.WriteAt(data, 0)

	// Track metadata writes for accurate write amplification calculation
	if err == nil {
		p.stats.pageWrites++
		p.stats.bytesWritten += int64(PageSize)
	}

	return err
}

// GetPage loads a page from cache or disk
func (p *Pager) GetPage(pageID uint32) (*Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrDatabaseClosed
	}

	// Check cache first
	if page, ok := p.cache[pageID]; ok {
		// Update LRU
		if elem, ok := p.lruMap[pageID]; ok {
			p.lru.MoveToFront(elem)
		}
		p.stats.cacheHits++ // Track cache hit
		return page, nil
	}

	// Load from disk
	page, err := p.readPage(pageID)
	if err != nil {
		return nil, err
	}

	// Add to cache
	p.addToCache(pageID, page)

	return page, nil
}

// readPage reads a page from disk
func (p *Pager) readPage(pageID uint32) (*Page, error) {
	if pageID >= p.metadata.NumPages {
		return nil, errors.New("page ID out of bounds")
	}

	offset := int64(pageID) * PageSize
	data := make([]byte, PageSize)

	n, err := p.file.ReadAt(data, offset)
	if err == nil {
		p.stats.pageReads++ // Track page read
	}
	if err != nil {
		return nil, err
	}
	if n != PageSize {
		return nil, errors.New("incomplete page read")
	}

	return LoadPage(pageID, data)
}

// writePage writes a page to disk
func (p *Pager) writePage(page *Page) error {
	offset := int64(page.ID()) * PageSize
	_, err := p.file.WriteAt(page.Data(), offset)

	// Track bytes written for write amplification calculation
	if err == nil {
		p.stats.pageWrites++
		p.stats.bytesWritten += int64(PageSize)
	}

	return err
}

// addToCache adds a page to the cache, evicting if necessary
func (p *Pager) addToCache(pageID uint32, page *Page) {
	// Evict if cache is full
	if p.lru.Len() >= p.cacheSize {
		p.evictLRU()
	}

	// Add to cache
	p.cache[pageID] = page
	elem := p.lru.PushFront(&lruEntry{pageID: pageID})
	p.lruMap[pageID] = elem
}

// evictLRU evicts the least recently used page
func (p *Pager) evictLRU() {
	elem := p.lru.Back()
	if elem == nil {
		return
	}

	entry := elem.Value.(*lruEntry)
	pageID := entry.pageID

	// Flush if dirty
	if p.dirty[pageID] {
		if page, ok := p.cache[pageID]; ok {
			if err := p.writePage(page); err != nil {
				// Log error but continue
				fmt.Printf("error flushing page %d: %v\n", pageID, err)
			}
			page.SetDirty(false)
			delete(p.dirty, pageID)
		}
	}

	// Remove from cache
	delete(p.cache, pageID)
	delete(p.lruMap, pageID)
	p.lru.Remove(elem)
}

// NewPage allocates a new page
func (p *Pager) NewPage(pageType byte) (*Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrDatabaseClosed
	}

	var pageID uint32

	// Try to allocate from free list
	if p.metadata.FreeListPtr != 0 {
		// TODO: Implement free list allocation
		// For now, just allocate new pages
		pageID = p.metadata.NumPages
		p.metadata.NumPages++
	} else {
		// Allocate new page
		pageID = p.metadata.NumPages
		p.metadata.NumPages++
	}

	// Create new page
	page := NewPage(pageID, pageType)

	// Add to cache
	p.addToCache(pageID, page)
	p.dirty[pageID] = true

	// Note: Metadata is tracked in memory and will be written during Sync() or Close()
	// This avoids writing metadata on every page allocation (huge write amp reduction!)

	return page, nil
}

// MarkDirty marks a page as dirty
func (p *Pager) MarkDirty(pageID uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Log to WAL if enabled (before modifying)
	if p.wal != nil {
		if page, ok := p.cache[pageID]; ok {
			// Log the entire page to WAL
			_ = p.wal.LogPageWrite(pageID, 0, page.data[:])
		}
	}

	if page, ok := p.cache[pageID]; ok {
		page.SetDirty(true)
		p.dirty[pageID] = true
	}
}

// SetWAL sets the WAL for this pager
func (p *Pager) SetWAL(wal *WAL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wal = wal
}

// FreePage marks a page as free and adds it to the free list
func (p *Pager) FreePage(pageID uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from cache
	if _, ok := p.cache[pageID]; ok {
		delete(p.cache, pageID)

		// Remove from LRU
		if elem, ok := p.lruMap[pageID]; ok {
			p.lru.Remove(elem)
			delete(p.lruMap, pageID)
		}
	}

	// Remove from dirty set
	delete(p.dirty, pageID)

	// Add to free list (for future allocation)
	// For now, we'll just mark it as freed
	// A full implementation would maintain a free list in metadata
}

// Flush writes all dirty pages to disk
func (p *Pager) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrDatabaseClosed
	}

	for pageID := range p.dirty {
		if page, ok := p.cache[pageID]; ok {
			if err := p.writePage(page); err != nil {
				return fmt.Errorf("error flushing page %d: %w", pageID, err)
			}
			page.SetDirty(false)
		}
	}

	// Clear dirty set
	p.dirty = make(map[uint32]bool)

	return nil
}

// Sync flushes dirty pages and syncs to disk
func (p *Pager) Sync() error {
	if err := p.Flush(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrDatabaseClosed
	}

	// Write metadata to persist NumPages, RootPageID, etc.
	if err := p.writeMetadata(); err != nil {
		return err
	}

	return p.file.Sync()
}

// RootPageID returns the current root page ID
func (p *Pager) RootPageID() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metadata.RootPageID
}

// SetRootPageID sets the root page ID
func (p *Pager) SetRootPageID(pageID uint32) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.metadata.RootPageID = pageID
	return p.writeMetadata()
}

// NumPages returns the total number of pages
func (p *Pager) NumPages() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metadata.NumPages
}

// Close closes the pager and flushes all dirty pages
func (p *Pager) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	// Flush all dirty pages
	for pageID := range p.dirty {
		if page, ok := p.cache[pageID]; ok {
			if err := p.writePage(page); err != nil {
				return fmt.Errorf("error flushing page %d on close: %w", pageID, err)
			}
		}
	}

	// Write final metadata
	if err := p.writeMetadata(); err != nil {
		return err
	}

	// Sync to disk
	if err := p.file.Sync(); err != nil {
		return err
	}

	// Close file
	if err := p.file.Close(); err != nil {
		return err
	}

	p.closed = true
	return nil
}
