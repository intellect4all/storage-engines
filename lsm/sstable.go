package lsm

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

const (
	blockSize      = 4096 // 4KB blocks
	sstableMagic   = 0x5354424C // "STBL" in hex
)

// SSTableEntry represents a single entry in an SSTable
type SSTableEntry struct {
	Key      string
	Value    []byte
	Deleted  bool
}

// IndexEntry maps a key to its block offset
type IndexEntry struct {
	Key         string
	BlockOffset uint64
}

// SSTable is an immutable sorted file on disk
// File format:
// [Data Blocks (4KB each)]
// [Index Block]
// [Bloom Filter]
// [Footer]
type SSTable struct {
	file        *os.File
	path        string
	level       int
	fileNum     uint64
	minKey      string
	maxKey      string
	index       []IndexEntry
	bloomFilter *BloomFilter
	indexOffset uint64
	bloomOffset uint64
}

// Footer format: [indexOffset(8)][bloomOffset(8)][metadataOffset(8)][magic(4)]
const footerSize = 28

// OpenSSTable opens an existing SSTable and loads metadata into memory
func OpenSSTable(path string, level int, fileNum uint64) (*SSTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sstable: %w", err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat sstable: %w", err)
	}
	fileSize := stat.Size()

	if fileSize < footerSize {
		file.Close()
		return nil, fmt.Errorf("sstable file too small")
	}

	// Read footer
	footer := make([]byte, footerSize)
	_, err = file.ReadAt(footer, fileSize-footerSize)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read footer: %w", err)
	}

	// Verify magic number
	magic := binary.LittleEndian.Uint32(footer[24:])
	if magic != sstableMagic {
		file.Close()
		return nil, fmt.Errorf("invalid sstable magic number")
	}

	indexOffset := binary.LittleEndian.Uint64(footer[0:])
	bloomOffset := binary.LittleEndian.Uint64(footer[8:])
	metadataOffset := binary.LittleEndian.Uint64(footer[16:])

	// Read metadata (minKey and maxKey)
	metadataSize := bloomOffset - metadataOffset
	metadataData := make([]byte, metadataSize)
	_, err = file.ReadAt(metadataData, int64(metadataOffset))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	// Decode metadata
	minKey, maxKey, err := decodeMetadata(metadataData)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	// Read index
	indexSize := metadataOffset - indexOffset
	indexData := make([]byte, indexSize)
	_, err = file.ReadAt(indexData, int64(indexOffset))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	// Decode index
	index, err := decodeIndex(indexData)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to decode index: %w", err)
	}

	// Read bloom filter
	bloomSize := fileSize - int64(bloomOffset) - footerSize
	bloomData := make([]byte, bloomSize)
	_, err = file.ReadAt(bloomData, int64(bloomOffset))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read bloom filter: %w", err)
	}
	bloomFilter := DecodeBloomFilter(bloomData)

	return &SSTable{
		file:        file,
		path:        path,
		level:       level,
		fileNum:     fileNum,
		minKey:      minKey,
		maxKey:      maxKey,
		index:       index,
		bloomFilter: bloomFilter,
		indexOffset: indexOffset,
		bloomOffset: bloomOffset,
	}, nil
}

// decodeMetadata decodes the metadata block containing minKey and maxKey
// Format: [minKeySize(4)][maxKeySize(4)][minKey][maxKey]
func decodeMetadata(data []byte) (string, string, error) {
	if len(data) < 8 {
		return "", "", fmt.Errorf("metadata too small")
	}

	minKeySize := binary.LittleEndian.Uint32(data[0:])
	maxKeySize := binary.LittleEndian.Uint32(data[4:])

	if len(data) < 8+int(minKeySize)+int(maxKeySize) {
		return "", "", fmt.Errorf("metadata truncated")
	}

	minKey := string(data[8 : 8+minKeySize])
	maxKey := string(data[8+minKeySize : 8+minKeySize+maxKeySize])

	return minKey, maxKey, nil
}

// decodeIndex decodes the index block
// Format: [numEntries(4)][entry1][entry2]...
// Entry: [keySize(4)][blockOffset(8)][key]
func decodeIndex(data []byte) ([]IndexEntry, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("index too small")
	}

	numEntries := binary.LittleEndian.Uint32(data[0:])
	entries := make([]IndexEntry, numEntries)

	offset := 4
	for i := uint32(0); i < numEntries; i++ {
		if offset+12 > len(data) {
			return nil, fmt.Errorf("index truncated")
		}

		keySize := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		blockOffset := binary.LittleEndian.Uint64(data[offset:])
		offset += 8

		if offset+int(keySize) > len(data) {
			return nil, fmt.Errorf("index truncated")
		}

		key := string(data[offset : offset+int(keySize)])
		offset += int(keySize)

		entries[i] = IndexEntry{
			Key:         key,
			BlockOffset: blockOffset,
		}
	}

	return entries, nil
}

// Get searches for a key in the SSTable
func (sst *SSTable) Get(key string) ([]byte, bool, error) {
	// Check bloom filter first
	if !sst.bloomFilter.MayContain(key) {
		return nil, false, nil
	}

	// Find the block that might contain the key
	blockIdx := sort.Search(len(sst.index), func(i int) bool {
		return sst.index[i].Key > key
	})

	// If key is greater than all index keys, check the last block
	if blockIdx == 0 {
		return nil, false, nil
	}
	blockIdx--

	// Read the block
	blockOffset := sst.index[blockIdx].BlockOffset
	block, err := sst.readBlock(blockOffset)
	if err != nil {
		return nil, false, err
	}

	// Search within the block
	return searchBlock(block, key)
}

// readBlock reads a data block from disk
func (sst *SSTable) readBlock(offset uint64) ([]byte, error) {
	block := make([]byte, blockSize)
	n, err := sst.file.ReadAt(block, int64(offset))
	if err != nil && err.Error() != "EOF" {
		return nil, err
	}
	return block[:n], nil
}

// searchBlock searches for a key within a data block
// Block format: [numEntries(4)][entry1][entry2]...
// Entry: [keySize(4)][valueSize(4)][deleted(1)][key][value]
func searchBlock(block []byte, key string) ([]byte, bool, error) {
	if len(block) < 4 {
		return nil, false, nil
	}

	numEntries := binary.LittleEndian.Uint32(block[0:])
	offset := 4

	for i := uint32(0); i < numEntries; i++ {
		if offset+9 > len(block) {
			return nil, false, fmt.Errorf("block truncated")
		}

		keySize := binary.LittleEndian.Uint32(block[offset:])
		offset += 4
		valueSize := binary.LittleEndian.Uint32(block[offset:])
		offset += 4
		deleted := block[offset] == 1
		offset += 1

		if offset+int(keySize)+int(valueSize) > len(block) {
			return nil, false, fmt.Errorf("block truncated")
		}

		entryKey := string(block[offset : offset+int(keySize)])
		offset += int(keySize)

		if entryKey == key {
			if deleted {
				return nil, false, nil
			}
			value := make([]byte, valueSize)
			copy(value, block[offset:offset+int(valueSize)])
			return value, true, nil
		}

		offset += int(valueSize)

		// Early exit if we've passed the key (block is sorted)
		if entryKey > key {
			return nil, false, nil
		}
	}

	return nil, false, nil
}

// Overlaps checks if this SSTable's key range overlaps with [start, end]
func (sst *SSTable) Overlaps(start, end string) bool {
	if start != "" && sst.maxKey < start {
		return false
	}
	if end != "" && sst.minKey > end {
		return false
	}
	return true
}

// Close closes the SSTable file
func (sst *SSTable) Close() error {
	if sst.file != nil {
		return sst.file.Close()
	}
	return nil
}

// Remove deletes the SSTable file
func (sst *SSTable) Remove() error {
	sst.Close()
	return os.Remove(sst.path)
}

// MinKey returns the smallest key in the SSTable
func (sst *SSTable) MinKey() string {
	return sst.minKey
}

// MaxKey returns the largest key in the SSTable
func (sst *SSTable) MaxKey() string {
	return sst.maxKey
}

// Level returns the level of this SSTable
func (sst *SSTable) Level() int {
	return sst.level
}

// FileNum returns the file number of this SSTable
func (sst *SSTable) FileNum() uint64 {
	return sst.fileNum
}

// Path returns the file path
func (sst *SSTable) Path() string {
	return sst.path
}
