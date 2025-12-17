package lsm

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"log"
	"path/filepath"
)

// CompactionEntry represents an entry during compaction
type CompactionEntry struct {
	Key      string
	Value    []byte
	Sequence uint64
	Deleted  bool
	sstIndex int // Which SSTable this came from
}

// CompactionHeap implements a min-heap for k-way merge
type CompactionHeap []CompactionEntry

func (h CompactionHeap) Len() int { return len(h) }
func (h CompactionHeap) Less(i, j int) bool {
	if h[i].Key != h[j].Key {
		return h[i].Key < h[j].Key
	}
	// If keys are equal, prefer higher sequence number (newer)
	return h[i].Sequence > h[j].Sequence
}
func (h CompactionHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *CompactionHeap) Push(x interface{}) { *h = append(*h, x.(CompactionEntry)) }
func (h *CompactionHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// SSTableIterator iterates over entries in an SSTable
type SSTableIterator struct {
	sst          *SSTable
	blockIdx     int
	entryIdx     int
	currentBlock []byte
	entries      []CompactionEntry
}

// NewSSTableIterator creates an iterator for an SSTable
func NewSSTableIterator(sst *SSTable, sstIndex int) (*SSTableIterator, error) {
	it := &SSTableIterator{
		sst:      sst,
		blockIdx: 0,
		entryIdx: 0,
	}

	// Load first block
	if len(sst.index) > 0 {
		if err := it.loadBlock(0); err != nil {
			return nil, err
		}
	}

	return it, nil
}

// loadBlock loads a block and parses its entries
func (it *SSTableIterator) loadBlock(blockIdx int) error {
	if blockIdx >= len(it.sst.index) {
		return nil
	}

	blockOffset := it.sst.index[blockIdx].BlockOffset
	block, err := it.sst.readBlock(blockOffset)
	if err != nil {
		return err
	}

	it.currentBlock = block
	it.blockIdx = blockIdx
	it.entryIdx = 0
	it.entries = nil

	// Parse all entries in the block
	if len(block) < 4 {
		return nil
	}

	numEntries := binary.LittleEndian.Uint32(block[0:])
	offset := 4

	for i := uint32(0); i < numEntries; i++ {
		if offset+9 > len(block) {
			break
		}

		keySize := binary.LittleEndian.Uint32(block[offset:])
		offset += 4
		valueSize := binary.LittleEndian.Uint32(block[offset:])
		offset += 4
		deleted := block[offset] == 1
		offset += 1

		if offset+int(keySize)+int(valueSize) > len(block) {
			break
		}

		key := string(block[offset : offset+int(keySize)])
		offset += int(keySize)
		value := make([]byte, valueSize)
		copy(value, block[offset:offset+int(valueSize)])
		offset += int(valueSize)

		it.entries = append(it.entries, CompactionEntry{
			Key:      key,
			Value:    value,
			Deleted:  deleted,
			Sequence: 0, // SSTables don't store sequence, we'll use file order
		})
	}

	return nil
}

// Next advances to the next entry
func (it *SSTableIterator) Next() (CompactionEntry, bool) {
	if it.entryIdx < len(it.entries) {
		entry := it.entries[it.entryIdx]
		it.entryIdx++
		return entry, true
	}

	// Try to load next block
	it.blockIdx++
	if it.blockIdx >= len(it.sst.index) {
		return CompactionEntry{}, false
	}

	if err := it.loadBlock(it.blockIdx); err != nil {
		return CompactionEntry{}, false
	}

	if it.entryIdx < len(it.entries) {
		entry := it.entries[it.entryIdx]
		it.entryIdx++
		return entry, true
	}

	return CompactionEntry{}, false
}

// CompactL0ToL1 merges all L0 SSTables into L1
// Returns: new L1 files, old L1 files that were compacted, error
func CompactL0ToL1(dataDir string, l0Files, l1Files []*SSTable, nextFileNum *uint64) ([]*SSTable, []*SSTable, error) {
	if len(l0Files) == 0 {
		return nil, nil, nil
	}

	// Find overlapping L1 files
	minKey := l0Files[0].MinKey()
	maxKey := l0Files[0].MaxKey()
	for _, sst := range l0Files {
		if sst.MinKey() < minKey {
			minKey = sst.MinKey()
		}
		if sst.MaxKey() > maxKey {
			maxKey = sst.MaxKey()
		}
	}

	var overlappingL1 []*SSTable
	for _, sst := range l1Files {
		if sst.Overlaps(minKey, maxKey) {
			overlappingL1 = append(overlappingL1, sst)
		}
	}

	// Merge all files
	allFiles := append(l0Files, overlappingL1...)
	newFiles, err := mergeFiles(dataDir, allFiles, 1, nextFileNum)
	if err != nil {
		return nil, nil, err
	}

	return newFiles, overlappingL1, nil
}

// CompactLnToLn1 compacts files from level n to level n+1
// Returns: new files at target level, old files from target level that were compacted, error
func CompactLnToLn1(dataDir string, lnFiles, ln1Files []*SSTable, targetLevel int, nextFileNum *uint64) ([]*SSTable, []*SSTable, error) {
	if len(lnFiles) == 0 {
		return nil, nil, nil
	}

	// Find overlapping files in next level
	minKey := lnFiles[0].MinKey()
	maxKey := lnFiles[0].MaxKey()
	for _, sst := range lnFiles {
		if sst.MinKey() < minKey {
			minKey = sst.MinKey()
		}
		if sst.MaxKey() > maxKey {
			maxKey = sst.MaxKey()
		}
	}

	var overlapping []*SSTable
	for _, sst := range ln1Files {
		if sst.Overlaps(minKey, maxKey) {
			overlapping = append(overlapping, sst)
		}
	}

	// Merge all files
	allFiles := append(lnFiles, overlapping...)
	newFiles, err := mergeFiles(dataDir, allFiles, targetLevel, nextFileNum)
	if err != nil {
		return nil, nil, err
	}

	return newFiles, overlapping, nil
}

// mergeFiles performs k-way merge of multiple SSTables
func mergeFiles(dataDir string, sstables []*SSTable, targetLevel int, nextFileNum *uint64) ([]*SSTable, error) {
	// Create iterators for each SSTable
	iterators := make([]*SSTableIterator, len(sstables))
	for i, sst := range sstables {
		it, err := NewSSTableIterator(sst, i)
		if err != nil {
			return nil, err
		}
		iterators[i] = it
	}

	// Initialize heap with first entry from each iterator
	h := &CompactionHeap{}
	heap.Init(h)

	for i, it := range iterators {
		if entry, ok := it.Next(); ok {
			entry.sstIndex = i
			heap.Push(h, entry)
		}
	}

	// Merge entries into new SSTables
	var newSSTables []*SSTable
	var builder *SSTableBuilder
	var currentFileNum uint64
	var entriesInFile int
	const maxEntriesPerFile = 100000 // ~4MB with 40-byte entries

	for h.Len() > 0 {
		// Get smallest entry
		entry := heap.Pop(h).(CompactionEntry)

		// Advance the iterator that produced this entry
		it := iterators[entry.sstIndex]
		if nextEntry, ok := it.Next(); ok {
			nextEntry.sstIndex = entry.sstIndex
			heap.Push(h, nextEntry)
		}

		// Skip duplicates (keep only the first occurrence, which has highest sequence)
		if h.Len() > 0 {
			peek := (*h)[0]
			if peek.Key == entry.Key {
				// Skip this entry, we'll process the newer one
				continue
			}
		}

		// Drop tombstones in final level (L4)
		if targetLevel == 4 && entry.Deleted {
			continue
		}

		// Create new builder if needed
		if builder == nil {
			currentFileNum = *nextFileNum
			*nextFileNum++
			path := filepath.Join(dataDir, fmt.Sprintf("L%d-%06d.sst", targetLevel, currentFileNum))
			var err error
			builder, err = NewSSTableBuilder(path, maxEntriesPerFile)
			if err != nil {
				return nil, err
			}
			entriesInFile = 0
		}

		// Add entry to current builder
		if err := builder.Add(entry.Key, entry.Value, entry.Deleted); err != nil {
			builder.Abort()
			return nil, err
		}
		entriesInFile++

		// Finish file if it's getting large
		if entriesInFile >= maxEntriesPerFile {
			if err := builder.Finish(); err != nil {
				return nil, err
			}

			// Open the newly created SSTable
			path := filepath.Join(dataDir, fmt.Sprintf("L%d-%06d.sst", targetLevel, currentFileNum))
			sst, err := OpenSSTable(path, targetLevel, currentFileNum)
			if err != nil {
				return nil, err
			}
			newSSTables = append(newSSTables, sst)

			builder = nil
		}
	}

	// Finish last file
	if builder != nil {
		if err := builder.Finish(); err != nil {
			return nil, err
		}

		path := filepath.Join(dataDir, fmt.Sprintf("L%d-%06d.sst", targetLevel, currentFileNum))
		sst, err := OpenSSTable(path, targetLevel, currentFileNum)
		if err != nil {
			return nil, err
		}
		newSSTables = append(newSSTables, sst)
	}

	return newSSTables, nil
}

// DeleteSSTables deletes a list of SSTables from disk
func DeleteSSTables(sstables []*SSTable) error {
	for _, sst := range sstables {
		if err := sst.Remove(); err != nil {
			log.Printf("Warning: failed to delete SSTable %s: %v", sst.Path(), err)
		}
	}
	return nil
}
