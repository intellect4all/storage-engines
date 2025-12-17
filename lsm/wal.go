package lsm

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// WAL is a Write-Ahead Log for durability
// Record format: [crc32][sequence][keySize][valueSize][deleted][key][value]
type WAL struct {
	file *os.File
	path string
}

// NewWAL creates a new write-ahead log
func NewWAL(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	return &WAL{
		file: file,
		path: path,
	}, nil
}

// Append writes a record to the WAL
func (w *WAL) Append(key string, value []byte, seq uint64, deleted bool) error {
	// Calculate sizes
	keySize := uint32(len(key))
	valueSize := uint32(len(value))

	// Build record (without CRC first)
	recordSize := 4 + 8 + 4 + 4 + 1 + int(keySize) + int(valueSize)
	record := make([]byte, recordSize)

	offset := 4 // Skip CRC for now
	binary.LittleEndian.PutUint64(record[offset:], seq)
	offset += 8
	binary.LittleEndian.PutUint32(record[offset:], keySize)
	offset += 4
	binary.LittleEndian.PutUint32(record[offset:], valueSize)
	offset += 4
	if deleted {
		record[offset] = 1
	} else {
		record[offset] = 0
	}
	offset += 1
	copy(record[offset:], key)
	offset += int(keySize)
	copy(record[offset:], value)

	// Calculate and write CRC
	crc := crc32.ChecksumIEEE(record[4:])
	binary.LittleEndian.PutUint32(record[0:], crc)

	// Write to file
	_, err := w.file.Write(record)
	return err
}

// Sync forces a sync to disk
func (w *WAL) Sync() error {
	return w.file.Sync()
}

// Close closes the WAL file
func (w *WAL) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// WALEntry represents a recovered entry from the WAL
type WALEntry struct {
	Key      string
	Value    []byte
	Sequence uint64
	Deleted  bool
}

// ReadAll reads all entries from the WAL for recovery
func (w *WAL) ReadAll() ([]WALEntry, error) {
	// Seek to beginning
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek WAL: %w", err)
	}

	var entries []WALEntry
	buf := make([]byte, 1024*1024) // 1MB buffer

	for {
		// Read header (CRC + sequence + keySise + valueSize + deleted)
		header := make([]byte, 21)
		_, err := io.ReadFull(w.file, header)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read WAL header: %w", err)
		}

		// Parse header
		crc := binary.LittleEndian.Uint32(header[0:])
		seq := binary.LittleEndian.Uint64(header[4:])
		keySize := binary.LittleEndian.Uint32(header[12:])
		valueSize := binary.LittleEndian.Uint32(header[16:])
		deleted := header[20] == 1

		// Read key and value
		dataSize := int(keySize + valueSize)
		if dataSize > len(buf) {
			buf = make([]byte, dataSize)
		}
		data := buf[:dataSize]
		_, err = io.ReadFull(w.file, data)
		if err != nil {
			return nil, fmt.Errorf("failed to read WAL data: %w", err)
		}

		// Verify CRC
		recordData := make([]byte, 17+dataSize)
		copy(recordData, header[4:])
		copy(recordData[17:], data)
		expectedCRC := crc32.ChecksumIEEE(recordData)
		if crc != expectedCRC {
			return nil, fmt.Errorf("WAL corruption detected: CRC mismatch")
		}

		// Extract key and value
		key := string(data[:keySize])
		value := make([]byte, valueSize)
		copy(value, data[keySize:])

		entries = append(entries, WALEntry{
			Key:      key,
			Value:    value,
			Sequence: seq,
			Deleted:  deleted,
		})
	}

	return entries, nil
}

// Delete removes the WAL file
func (w *WAL) Delete() error {
	w.Close()
	return os.Remove(w.path)
}
