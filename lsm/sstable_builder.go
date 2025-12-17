package lsm

import (
	"encoding/binary"
	"fmt"
	"os"
)

// SSTableBuilder constructs a new SSTable from sorted entries
type SSTableBuilder struct {
	file         *os.File
	path         string
	currentBlock []byte
	blockOffset  uint64
	index        []IndexEntry
	bloomFilter  *BloomFilter
	minKey       string
	maxKey       string
	numEntries   int
}

// NewSSTableBuilder creates a new SSTable builder
func NewSSTableBuilder(path string, expectedKeys int) (*SSTableBuilder, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create sstable: %w", err)
	}

	// Create bloom filter with 1% false positive rate
	bloomFilter := NewBloomFilter(expectedKeys, 0.01)

	return &SSTableBuilder{
		file:         file,
		path:         path,
		currentBlock: make([]byte, 4), // Start with numEntries = 0
		blockOffset:  0,
		index:        make([]IndexEntry, 0),
		bloomFilter:  bloomFilter,
	}, nil
}

// Add adds a key-value pair to the SSTable
// MUST be called in sorted key order!
func (b *SSTableBuilder) Add(key string, value []byte, deleted bool) error {
	// Track min/max keys
	if b.numEntries == 0 {
		b.minKey = key
	}
	b.maxKey = key
	b.numEntries++

	// Add to bloom filter
	b.bloomFilter.Add(key)

	// Encode entry: [keySize(4)][valueSize(4)][deleted(1)][key][value]
	keySize := uint32(len(key))
	valueSize := uint32(len(value))
	entrySize := 4 + 4 + 1 + int(keySize) + int(valueSize)

	entry := make([]byte, entrySize)
	offset := 0
	binary.LittleEndian.PutUint32(entry[offset:], keySize)
	offset += 4
	binary.LittleEndian.PutUint32(entry[offset:], valueSize)
	offset += 4
	if deleted {
		entry[offset] = 1
	} else {
		entry[offset] = 0
	}
	offset += 1
	copy(entry[offset:], key)
	offset += int(keySize)
	copy(entry[offset:], value)

	// Check if adding this entry would exceed block size
	if len(b.currentBlock)+entrySize > blockSize {
		// Flush current block
		if err := b.flushBlock(); err != nil {
			return err
		}
	}

	// Add entry to current block
	b.currentBlock = append(b.currentBlock, entry...)

	return nil
}

// flushBlock writes the current block to disk and adds an index entry
func (b *SSTableBuilder) flushBlock() error {
	if len(b.currentBlock) <= 4 {
		// Empty block (only has numEntries header)
		return nil
	}

	// Get the first key from the block for the index
	firstKey, err := b.getFirstKeyFromBlock()
	if err != nil {
		return err
	}

	// Update block header with entry count
	numEntriesInBlock := b.getNumEntriesInBlock()
	binary.LittleEndian.PutUint32(b.currentBlock[0:], numEntriesInBlock)

	// Write block to file
	_, err = b.file.Write(b.currentBlock)
	if err != nil {
		return fmt.Errorf("failed to write block: %w", err)
	}

	// Add index entry
	b.index = append(b.index, IndexEntry{
		Key:         firstKey,
		BlockOffset: b.blockOffset,
	})

	// Update offset for next block
	b.blockOffset += uint64(len(b.currentBlock))

	// Pad block to blockSize if needed
	if len(b.currentBlock) < blockSize {
		padding := make([]byte, blockSize-len(b.currentBlock))
		_, err = b.file.Write(padding)
		if err != nil {
			return fmt.Errorf("failed to write padding: %w", err)
		}
		b.blockOffset += uint64(len(padding))
	}

	// Reset block
	b.currentBlock = make([]byte, 4)

	return nil
}

// getFirstKeyFromBlock extracts the first key from the current block
func (b *SSTableBuilder) getFirstKeyFromBlock() (string, error) {
	if len(b.currentBlock) < 13 { // 4 (numEntries) + 4 (keySize) + 4 (valueSize) + 1 (deleted)
		return "", fmt.Errorf("block too small")
	}

	offset := 4 // Skip numEntries
	keySize := binary.LittleEndian.Uint32(b.currentBlock[offset:])
	offset += 4
	offset += 4 // Skip valueSize
	offset += 1 // Skip deleted

	if offset+int(keySize) > len(b.currentBlock) {
		return "", fmt.Errorf("block truncated")
	}

	return string(b.currentBlock[offset : offset+int(keySize)]), nil
}

// getNumEntriesInBlock counts entries in the current block
func (b *SSTableBuilder) getNumEntriesInBlock() uint32 {
	count := uint32(0)
	offset := 4 // Skip numEntries header

	for offset < len(b.currentBlock) {
		if offset+9 > len(b.currentBlock) {
			break
		}

		keySize := binary.LittleEndian.Uint32(b.currentBlock[offset:])
		offset += 4
		valueSize := binary.LittleEndian.Uint32(b.currentBlock[offset:])
		offset += 4
		offset += 1 // deleted

		if offset+int(keySize)+int(valueSize) > len(b.currentBlock) {
			break
		}

		offset += int(keySize) + int(valueSize)
		count++
	}

	return count
}

// Finish flushes remaining data and writes index, bloom filter, and footer
func (b *SSTableBuilder) Finish() error {
	// Flush any remaining block
	if len(b.currentBlock) > 4 {
		if err := b.flushBlock(); err != nil {
			return err
		}
	}

	// Remember index offset
	indexOffset := b.blockOffset

	// Write index block
	indexData := b.encodeIndex()
	_, err := b.file.Write(indexData)
	if err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	// Remember metadata offset
	metadataOffset := b.blockOffset + uint64(len(indexData))

	// Write metadata (minKey and maxKey)
	metadataData := b.encodeMetadata()
	_, err = b.file.Write(metadataData)
	if err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Remember bloom filter offset
	bloomOffset := metadataOffset + uint64(len(metadataData))

	// Write bloom filter
	bloomData := b.bloomFilter.Encode()
	_, err = b.file.Write(bloomData)
	if err != nil {
		return fmt.Errorf("failed to write bloom filter: %w", err)
	}

	// Write footer: [indexOffset(8)][bloomOffset(8)][metadataOffset(8)][magic(4)]
	footer := make([]byte, footerSize)
	binary.LittleEndian.PutUint64(footer[0:], indexOffset)
	binary.LittleEndian.PutUint64(footer[8:], bloomOffset)
	binary.LittleEndian.PutUint64(footer[16:], metadataOffset)
	binary.LittleEndian.PutUint32(footer[24:], sstableMagic)

	_, err = b.file.Write(footer)
	if err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	// Sync to disk
	err = b.file.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync sstable: %w", err)
	}

	return b.file.Close()
}

// encodeMetadata encodes the metadata block
// Format: [minKeySize(4)][maxKeySize(4)][minKey][maxKey]
func (b *SSTableBuilder) encodeMetadata() []byte {
	minKeySize := uint32(len(b.minKey))
	maxKeySize := uint32(len(b.maxKey))

	size := 4 + 4 + int(minKeySize) + int(maxKeySize)
	buf := make([]byte, size)

	binary.LittleEndian.PutUint32(buf[0:], minKeySize)
	binary.LittleEndian.PutUint32(buf[4:], maxKeySize)
	copy(buf[8:], b.minKey)
	copy(buf[8+minKeySize:], b.maxKey)

	return buf
}

// encodeIndex encodes the index block
// Format: [numEntries(4)][entry1][entry2]...
// Entry: [keySize(4)][blockOffset(8)][key]
func (b *SSTableBuilder) encodeIndex() []byte {
	// Calculate size
	size := 4 // numEntries
	for _, entry := range b.index {
		size += 4 + 8 + len(entry.Key)
	}

	buf := make([]byte, size)
	offset := 0

	// Write numEntries
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(b.index)))
	offset += 4

	// Write entries
	for _, entry := range b.index {
		keySize := uint32(len(entry.Key))
		binary.LittleEndian.PutUint32(buf[offset:], keySize)
		offset += 4
		binary.LittleEndian.PutUint64(buf[offset:], entry.BlockOffset)
		offset += 8
		copy(buf[offset:], entry.Key)
		offset += int(keySize)
	}

	return buf
}

// Abort closes and deletes the SSTable file
func (b *SSTableBuilder) Abort() error {
	b.file.Close()
	return os.Remove(b.path)
}
