package btree

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

// WAL implements a physical Write-Ahead Log for crash recovery
// Physical WAL records actual byte-level changes to pages, not logical operations
type WAL struct {
	file     *os.File
	mu       sync.Mutex
	offset   int64
	flushed  int64 // Last fsynced offset
	filePath string
}

// WAL Record Types
const (
	WALRecordPageWrite  = 1 // Page modification
	WALRecordCheckpoint = 2 // Checkpoint marker
	WALRecordCommit     = 3 // Transaction commit
)

// WALRecord represents a single WAL entry
type WALRecord struct {
	Type     uint8
	PageID   uint32
	Offset   uint32 // Offset within page
	Length   uint32 // Length of data
	Data     []byte // Actual data to write
	Checksum uint32 // CRC32 checksum
}

// WAL file format:
// [Magic: "BWAL"][Version: 1]
// Each record:
// [Type(1)][PageID(4)][Offset(4)][Length(4)][Data(Length)][CRC32(4)]

const (
	WALMagic   = "BWAL"
	WALVersion = 1
	WALHeaderSize = 8 // Magic(4) + Version(4)
)

// NewWAL creates or opens a WAL file
func NewWAL(filePath string) (*WAL, error) {
	// Open WAL file (create if doesn't exist)
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	wal := &WAL{
		file:     file,
		filePath: filePath,
	}

	// Check if file is new or existing
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	if stat.Size() == 0 {
		// New file, write header
		if err := wal.writeHeader(); err != nil {
			file.Close()
			return nil, err
		}
		wal.offset = WALHeaderSize
		wal.flushed = WALHeaderSize
	} else {
		// Existing file, validate header
		if err := wal.validateHeader(); err != nil {
			file.Close()
			return nil, err
		}
		// Seek to end
		offset, err := file.Seek(0, io.SeekEnd)
		if err != nil {
			file.Close()
			return nil, err
		}
		wal.offset = offset
		wal.flushed = offset
	}

	return wal, nil
}

// writeHeader writes the WAL file header
func (w *WAL) writeHeader() error {
	header := make([]byte, WALHeaderSize)
	copy(header[0:4], []byte(WALMagic))
	binary.LittleEndian.PutUint32(header[4:8], WALVersion)

	_, err := w.file.WriteAt(header, 0)
	return err
}

// validateHeader validates the WAL file header
func (w *WAL) validateHeader() error {
	header := make([]byte, WALHeaderSize)
	if _, err := w.file.ReadAt(header, 0); err != nil {
		return fmt.Errorf("failed to read WAL header: %w", err)
	}

	if string(header[0:4]) != WALMagic {
		return fmt.Errorf("invalid WAL magic: %s", string(header[0:4]))
	}

	version := binary.LittleEndian.Uint32(header[4:8])
	if version != WALVersion {
		return fmt.Errorf("unsupported WAL version: %d", version)
	}

	return nil
}

// LogPageWrite logs a page modification to WAL
func (w *WAL) LogPageWrite(pageID uint32, offset uint32, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	record := &WALRecord{
		Type:   WALRecordPageWrite,
		PageID: pageID,
		Offset: offset,
		Length: uint32(len(data)),
		Data:   data,
	}

	// Calculate checksum
	record.Checksum = w.calculateChecksum(record)

	// Encode record
	encoded := w.encodeRecord(record)

	// Write to WAL file
	if _, err := w.file.WriteAt(encoded, w.offset); err != nil {
		return fmt.Errorf("failed to write WAL record: %w", err)
	}

	w.offset += int64(len(encoded))
	return nil
}

// LogCheckpoint writes a checkpoint marker
func (w *WAL) LogCheckpoint() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	record := &WALRecord{
		Type: WALRecordCheckpoint,
	}

	record.Checksum = w.calculateChecksum(record)
	encoded := w.encodeRecord(record)

	if _, err := w.file.WriteAt(encoded, w.offset); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	w.offset += int64(len(encoded))
	return nil
}

// encodeRecord encodes a WAL record to bytes
func (w *WAL) encodeRecord(r *WALRecord) []byte {
	// Calculate total size
	size := 1 + 4 + 4 + 4 + len(r.Data) + 4 // type + pageID + offset + length + data + checksum
	buf := make([]byte, size)

	buf[0] = r.Type
	binary.LittleEndian.PutUint32(buf[1:5], r.PageID)
	binary.LittleEndian.PutUint32(buf[5:9], r.Offset)
	binary.LittleEndian.PutUint32(buf[9:13], r.Length)

	if len(r.Data) > 0 {
		copy(buf[13:13+len(r.Data)], r.Data)
	}

	binary.LittleEndian.PutUint32(buf[size-4:], r.Checksum)

	return buf
}

// decodeRecord decodes a WAL record from bytes
func (w *WAL) decodeRecord(buf []byte) (*WALRecord, error) {
	if len(buf) < 17 { // Minimum size: type(1) + pageID(4) + offset(4) + length(4) + checksum(4)
		return nil, fmt.Errorf("record too short: %d bytes", len(buf))
	}

	record := &WALRecord{
		Type:   buf[0],
		PageID: binary.LittleEndian.Uint32(buf[1:5]),
		Offset: binary.LittleEndian.Uint32(buf[5:9]),
		Length: binary.LittleEndian.Uint32(buf[9:13]),
	}

	if record.Length > 0 {
		if len(buf) < 13+int(record.Length)+4 {
			return nil, fmt.Errorf("incomplete record: expected %d bytes, got %d", 13+int(record.Length)+4, len(buf))
		}
		record.Data = make([]byte, record.Length)
		copy(record.Data, buf[13:13+record.Length])
	}

	record.Checksum = binary.LittleEndian.Uint32(buf[13+record.Length:])

	// Validate checksum
	expectedChecksum := w.calculateChecksum(record)
	if record.Checksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %d, got %d", expectedChecksum, record.Checksum)
	}

	return record, nil
}

// calculateChecksum computes CRC32 checksum for a record
func (w *WAL) calculateChecksum(r *WALRecord) uint32 {
	h := crc32.NewIEEE()

	buf := make([]byte, 13)
	buf[0] = r.Type
	binary.LittleEndian.PutUint32(buf[1:5], r.PageID)
	binary.LittleEndian.PutUint32(buf[5:9], r.Offset)
	binary.LittleEndian.PutUint32(buf[9:13], r.Length)

	h.Write(buf)
	if len(r.Data) > 0 {
		h.Write(r.Data)
	}

	return h.Sum32()
}

// Sync forces all buffered WAL records to disk
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	w.flushed = w.offset
	return nil
}

// ReadAll reads all WAL records (for recovery)
func (w *WAL) ReadAll() ([]*WALRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var records []*WALRecord
	offset := int64(WALHeaderSize)

	for offset < w.offset {
		// Read record header to determine size
		header := make([]byte, 13)
		if _, err := w.file.ReadAt(header, offset); err != nil {
			if err == io.EOF {
				break
			}
			return records, fmt.Errorf("failed to read record header at offset %d: %w", offset, err)
		}

		recordType := header[0]
		length := binary.LittleEndian.Uint32(header[9:13])

		// Read full record
		recordSize := 13 + int(length) + 4 // header + data + checksum
		fullRecord := make([]byte, recordSize)
		if _, err := w.file.ReadAt(fullRecord, offset); err != nil {
			if err == io.EOF {
				break
			}
			return records, fmt.Errorf("failed to read full record at offset %d: %w", offset, err)
		}

		// Decode record
		record, err := w.decodeRecord(fullRecord)
		if err != nil {
			// Corrupted record, stop reading
			return records, fmt.Errorf("corrupted record at offset %d: %w", offset, err)
		}

		records = append(records, record)
		offset += int64(recordSize)

		// Stop at checkpoint
		if recordType == WALRecordCheckpoint {
			break
		}
	}

	return records, nil
}

// Truncate removes all WAL records (after checkpoint)
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Create new file with just header
	file, err := os.OpenFile(w.filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	w.file = file

	if err := w.writeHeader(); err != nil {
		return err
	}

	w.offset = WALHeaderSize
	w.flushed = WALHeaderSize

	return nil
}

// Close closes the WAL file
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Sync(); err != nil {
		return err
	}

	return w.file.Close()
}

// Size returns the current WAL size in bytes
func (w *WAL) Size() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.offset
}
