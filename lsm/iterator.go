package lsm

import (
	"container/heap"
)

// Iterator provides sequential access to key-value pairs in sorted order
type Iterator interface {
	// SeekToFirst positions the iterator at the first key
	SeekToFirst()
	// Valid returns true if the iterator is positioned at a valid entry
	Valid() bool
	// Next advances to the next entry
	Next()
	// Key returns the current key
	Key() string
	// Value returns the current value
	Value() []byte
	// Error returns any error that occurred
	Error() error
}

// MemTableIterator iterates over a memtable
type MemTableIterator struct {
	entries []MemTableEntry
	index   int
}

// NewMemTableIterator creates an iterator for a memtable
func NewMemTableIterator(memtable *MemTable) *MemTableIterator {
	return &MemTableIterator{
		entries: memtable.GetAllEntries(),
		index:   -1,
	}
}

func (it *MemTableIterator) SeekToFirst() {
	it.index = 0
}

func (it *MemTableIterator) Valid() bool {
	return it.index >= 0 && it.index < len(it.entries) && !it.entries[it.index].Deleted
}

func (it *MemTableIterator) Next() {
	it.index++
	// Skip deleted entries
	for it.index < len(it.entries) && it.entries[it.index].Deleted {
		it.index++
	}
}

func (it *MemTableIterator) Key() string {
	if !it.Valid() {
		return ""
	}
	return it.entries[it.index].Key
}

func (it *MemTableIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.entries[it.index].Value
}

func (it *MemTableIterator) Error() error {
	return nil
}

// MergingIteratorEntry represents an entry in the merging iterator heap
type MergingIteratorEntry struct {
	key      string
	value    []byte
	sequence uint64
	iter     Iterator
	priority int // Lower priority = checked first (memtable > L0 > L1 > L2)
}

// MergingIteratorHeap implements a min-heap for merging multiple iterators
type MergingIteratorHeap []MergingIteratorEntry

func (h MergingIteratorHeap) Len() int { return len(h) }
func (h MergingIteratorHeap) Less(i, j int) bool {
	// First compare by key
	if h[i].key != h[j].key {
		return h[i].key < h[j].key
	}
	// If keys are equal, prefer lower priority (newer data)
	return h[i].priority < h[j].priority
}
func (h MergingIteratorHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *MergingIteratorHeap) Push(x interface{}) { *h = append(*h, x.(MergingIteratorEntry)) }
func (h *MergingIteratorHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// MergingIterator merges multiple sorted iterators
type MergingIterator struct {
	iterators []Iterator
	priorities []int
	heap      *MergingIteratorHeap
	currentKey string
	currentValue []byte
	err error
}

// NewMergingIterator creates a merging iterator from multiple iterators
// Iterators should be ordered by priority (0 = highest priority, checked first)
func NewMergingIterator(iterators []Iterator, priorities []int) *MergingIterator {
	it := &MergingIterator{
		iterators:  iterators,
		priorities: priorities,
		heap:       &MergingIteratorHeap{},
	}

	heap.Init(it.heap)
	return it
}

func (it *MergingIterator) SeekToFirst() {
	// Initialize heap with first entry from each iterator
	*it.heap = (*it.heap)[:0]
	heap.Init(it.heap)

	for i, iter := range it.iterators {
		iter.SeekToFirst()
		if iter.Valid() {
			heap.Push(it.heap, MergingIteratorEntry{
				key:      iter.Key(),
				value:    iter.Value(),
				iter:     iter,
				priority: it.priorities[i],
			})
		}
	}

	// Advance to first entry
	it.Next()
}

func (it *MergingIterator) Valid() bool {
	return it.currentKey != ""
}

func (it *MergingIterator) Next() {
	if it.heap.Len() == 0 {
		it.currentKey = ""
		it.currentValue = nil
		return
	}

	// Get smallest entry
	entry := heap.Pop(it.heap).(MergingIteratorEntry)
	it.currentKey = entry.key
	it.currentValue = entry.value

	// Advance the iterator that produced this entry
	entry.iter.Next()
	if entry.iter.Valid() {
		heap.Push(it.heap, MergingIteratorEntry{
			key:      entry.iter.Key(),
			value:    entry.iter.Value(),
			iter:     entry.iter,
			priority: entry.priority,
		})
	}

	// Skip duplicate keys (keep only the first, which has highest priority)
	for it.heap.Len() > 0 {
		peek := (*it.heap)[0]
		if peek.key != it.currentKey {
			break
		}

		// Duplicate key, skip it
		entry := heap.Pop(it.heap).(MergingIteratorEntry)
		entry.iter.Next()
		if entry.iter.Valid() {
			heap.Push(it.heap, MergingIteratorEntry{
				key:      entry.iter.Key(),
				value:    entry.iter.Value(),
				iter:     entry.iter,
				priority: entry.priority,
			})
		}
	}
}

func (it *MergingIterator) Key() string {
	return it.currentKey
}

func (it *MergingIterator) Value() []byte {
	return it.currentValue
}

func (it *MergingIterator) Error() error {
	return it.err
}

// Scan returns an iterator over the key range [start, end]
// If start is empty, starts from the beginning
// If end is empty, continues to the end
func (lsm *LSM) Scan(start, end string) Iterator {
	var iterators []Iterator
	var priorities []int
	priority := 0

	// Add active memtable iterator
	lsm.mu.RLock()
	activeIter := NewMemTableIterator(lsm.activeMemtable)
	iterators = append(iterators, activeIter)
	priorities = append(priorities, priority)
	priority++

	// Add immutable memtable iterator if present
	if lsm.immutableMemtable != nil {
		immutableIter := NewMemTableIterator(lsm.immutableMemtable)
		iterators = append(iterators, immutableIter)
		priorities = append(priorities, priority)
		priority++
	}
	lsm.mu.RUnlock()

	// Add SSTable iterators from each level
	// Note: In a full implementation, we'd create proper SSTable iterators
	// For now, this is a simplified version
	// TODO: Implement full SSTable iterator support

	mergingIter := NewMergingIterator(iterators, priorities)
	mergingIter.SeekToFirst()

	return mergingIter
}
