package lsm

import (
	"sort"
	"sync"
)

// MemTableEntry represents a single entry in the memtable
type MemTableEntry struct {
	Key      string
	Value    []byte
	Sequence uint64
	Deleted  bool
}

// MemTable is an in-memory sorted structure for storing recent writes
// It uses a sorted slice with binary search for simplicity
type MemTable struct {
	mu      sync.RWMutex
	entries []MemTableEntry
	size    int // Approximate size in bytes
	maxSize int // Maximum size before flush
}

// NewMemTable creates a new memtable with the given maximum size
func NewMemTable(maxSize int) *MemTable {
	return &MemTable{
		entries: make([]MemTableEntry, 0, 1024),
		maxSize: maxSize,
	}
}

// Put inserts a key-value pair with a sequence number
func (m *MemTable) Put(key string, value []byte, seq uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Binary search to find insertion point
	idx := sort.Search(len(m.entries), func(i int) bool {
		return m.entries[i].Key >= key
	})

	entry := MemTableEntry{
		Key:      key,
		Value:    value,
		Sequence: seq,
		Deleted:  false,
	}

	// If key exists at this position, replace it (same key)
	if idx < len(m.entries) && m.entries[idx].Key == key {
		oldSize := len(m.entries[idx].Value)
		m.entries[idx] = entry
		m.size += len(value) - oldSize
	} else {
		// Insert at the correct position
		m.entries = append(m.entries, MemTableEntry{})
		copy(m.entries[idx+1:], m.entries[idx:])
		m.entries[idx] = entry
		m.size += len(key) + len(value) + 16 // key + value + overhead
	}
}

// Delete marks a key as deleted with a tombstone
func (m *MemTable) Delete(key string, seq uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Binary search to find insertion point
	idx := sort.Search(len(m.entries), func(i int) bool {
		return m.entries[i].Key >= key
	})

	entry := MemTableEntry{
		Key:      key,
		Value:    nil,
		Sequence: seq,
		Deleted:  true,
	}

	// If key exists at this position, replace it
	if idx < len(m.entries) && m.entries[idx].Key == key {
		oldSize := len(m.entries[idx].Value)
		m.entries[idx] = entry
		m.size -= oldSize
	} else {
		// Insert tombstone at the correct position
		m.entries = append(m.entries, MemTableEntry{})
		copy(m.entries[idx+1:], m.entries[idx:])
		m.entries[idx] = entry
		m.size += len(key) + 16 // key + overhead
	}
}

// Get retrieves a value for a key
// Returns value, sequence number, deleted flag, and found status
func (m *MemTable) Get(key string) ([]byte, uint64, bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Binary search
	idx := sort.Search(len(m.entries), func(i int) bool {
		return m.entries[i].Key >= key
	})

	if idx < len(m.entries) && m.entries[idx].Key == key {
		entry := m.entries[idx]
		return entry.Value, entry.Sequence, entry.Deleted, true
	}

	return nil, 0, false, false
}

// Size returns the approximate size in bytes
func (m *MemTable) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

// IsFull returns true if the memtable has reached its maximum size
func (m *MemTable) IsFull() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size >= m.maxSize
}

// GetAllEntries returns all entries in sorted order for flushing to disk
func (m *MemTable) GetAllEntries() []MemTableEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Make a copy to avoid holding the lock during flush
	entries := make([]MemTableEntry, len(m.entries))
	copy(entries, m.entries)
	return entries
}

// Len returns the number of entries
func (m *MemTable) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
