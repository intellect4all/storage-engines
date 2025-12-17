package hashindex

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Record format on disk:
// [crc32(4)][timestamp(8)][keysize(4)][valuesize(4)][key][value]
const (
	headerSize = 4 + 8 + 4 + 4 // crc + timestamp + keysize + valuesize
)

// segment represents a single data file with reference counting
type segment struct {
	id   int
	path string

	// File handle (protected by atomic operations)
	file   atomic.Pointer[os.File]
	size   atomic.Int64
	closed atomic.Bool

	// Reference counting for safe deletion
	refCount atomic.Int32
	mu       sync.RWMutex // Protects file operations
}

func newSegment(id int, path string, file *os.File) *segment {
	s := &segment{
		id:   id,
		path: path,
	}
	s.file.Store(file)
	s.refCount.Store(1)
	return s
}

func (s *segment) acquire() bool {
	if s.closed.Load() {
		return false
	}

	s.refCount.Add(1)
	return true
}

func (s *segment) release() {
	if s.refCount.Add(-1) <= 0 {
		// Last reference gone, safe to close
		s.closeFile()
	}
}

// append writes a key-value pair to the segment
// Returns: offset, record size, error
func (s *segment) append(key, value []byte) (int64, int32, error) {
	if s.closed.Load() {
		return 0, 0, fmt.Errorf("segment closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file := s.file.Load()
	if file == nil {
		return 0, 0, fmt.Errorf("segment file closed")
	}

	// Build record
	timestamp := time.Now().Unix()
	keySize := uint32(len(key))
	valueSize := uint32(len(value))
	recordSize := headerSize + len(key) + len(value)

	// Calculate CRC (over timestamp, sizes, key, and value)
	crcData := make([]byte, 0, 8+4+4+len(key)+len(value))
	tmpBuf := make([]byte, 8)

	binary.LittleEndian.PutUint64(tmpBuf, uint64(timestamp))
	crcData = append(crcData, tmpBuf...)

	binary.LittleEndian.PutUint32(tmpBuf, keySize)
	crcData = append(crcData, tmpBuf[:4]...)

	binary.LittleEndian.PutUint32(tmpBuf, valueSize)
	crcData = append(crcData, tmpBuf[:4]...)

	crcData = append(crcData, key...)
	crcData = append(crcData, value...)

	crc := crc32.ChecksumIEEE(crcData)

	// Write record
	header := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(header[0:4], crc)
	binary.LittleEndian.PutUint64(header[4:12], uint64(timestamp))
	binary.LittleEndian.PutUint32(header[12:16], keySize)
	binary.LittleEndian.PutUint32(header[16:20], valueSize)

	offset := s.size.Load()

	// Write header
	if _, err := file.Write(header); err != nil {
		return 0, 0, err
	}

	// Write key and value
	if _, err := file.Write(key); err != nil {
		return 0, 0, err
	}

	if _, err := file.Write(value); err != nil {
		return 0, 0, err
	}

	s.size.Add(int64(recordSize))
	return offset, int32(recordSize), nil
}

// read reads a record at the given offset
// Returns the value (key is in the index)
func (s *segment) read(offset int64) ([]byte, error) {
	if !s.acquire() {
		return nil, fmt.Errorf("segment closed")
	}
	defer s.release()

	s.mu.RLock()
	defer s.mu.RUnlock()

	file := s.file.Load()
	if file == nil {
		return nil, fmt.Errorf("segment file closed")
	}

	// Read header
	header := make([]byte, headerSize)
	if _, err := file.ReadAt(header, offset); err != nil {
		return nil, err
	}

	crcStored := binary.LittleEndian.Uint32(header[0:4])
	timestamp := binary.LittleEndian.Uint64(header[4:12])
	keySize := binary.LittleEndian.Uint32(header[12:16])
	valueSize := binary.LittleEndian.Uint32(header[16:20])

	// Read key and value
	data := make([]byte, keySize+valueSize)
	if _, err := file.ReadAt(data, offset+headerSize); err != nil {
		return nil, err
	}

	// Verify CRC
	crcData := make([]byte, 0, 8+4+4+len(data))
	tmpBuf := make([]byte, 8)

	binary.LittleEndian.PutUint64(tmpBuf, timestamp)
	crcData = append(crcData, tmpBuf...)

	binary.LittleEndian.PutUint32(tmpBuf, keySize)
	crcData = append(crcData, tmpBuf[:4]...)

	binary.LittleEndian.PutUint32(tmpBuf, valueSize)
	crcData = append(crcData, tmpBuf[:4]...)

	crcData = append(crcData, data...)

	crcCalculated := crc32.ChecksumIEEE(crcData)
	if crcCalculated != crcStored {
		return nil, fmt.Errorf("CRC mismatch: stored=%x calculated=%x", crcStored, crcCalculated)
	}

	// Return value (skip key)
	value := data[keySize:]
	return value, nil
}

// readRecord reads a complete record (key and value) at the given offset
// Returns: key, value, next offset, error
// Used during compaction and recovery
func (s *segment) readRecord(offset int64) ([]byte, []byte, int64, error) {
	if !s.acquire() {
		return nil, nil, 0, fmt.Errorf("segment closed")
	}
	defer s.release()

	s.mu.RLock()
	defer s.mu.RUnlock()

	file := s.file.Load()
	if file == nil {
		return nil, nil, 0, fmt.Errorf("segment file closed")
	}

	// Read header
	header := make([]byte, headerSize)
	n, err := file.ReadAt(header, offset)
	if err != nil {
		if err == io.EOF && n == 0 {
			return nil, nil, 0, io.EOF
		}
		return nil, nil, 0, err
	}

	crcStored := binary.LittleEndian.Uint32(header[0:4])
	timestamp := binary.LittleEndian.Uint64(header[4:12])
	keySize := binary.LittleEndian.Uint32(header[12:16])
	valueSize := binary.LittleEndian.Uint32(header[16:20])

	// Read key and value
	data := make([]byte, keySize+valueSize)
	if _, err := file.ReadAt(data, offset+headerSize); err != nil {
		return nil, nil, 0, err
	}

	// Verify CRC
	crcData := make([]byte, 0, 8+4+4+len(data))
	tmpBuf := make([]byte, 8)

	binary.LittleEndian.PutUint64(tmpBuf, timestamp)
	crcData = append(crcData, tmpBuf...)

	binary.LittleEndian.PutUint32(tmpBuf, keySize)
	crcData = append(crcData, tmpBuf[:4]...)

	binary.LittleEndian.PutUint32(tmpBuf, valueSize)
	crcData = append(crcData, tmpBuf[:4]...)

	crcData = append(crcData, data...)

	crcCalculated := crc32.ChecksumIEEE(crcData)
	if crcCalculated != crcStored {
		return nil, nil, 0, fmt.Errorf("CRC mismatch")
	}

	key := data[:keySize]
	value := data[keySize:]
	nextOffset := offset + headerSize + int64(keySize) + int64(valueSize)

	return key, value, nextOffset, nil
}

// sync ensures all data is persisted to disk
func (s *segment) sync() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file := s.file.Load()
	if file == nil {
		return fmt.Errorf("segment file closed")
	}

	return file.Sync()
}

// closeFile closes the segment file (internal)
func (s *segment) closeFile() {
	if s.closed.Swap(true) {
		return // Already closed
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file := s.file.Load()
	if file != nil {
		file.Close()
		s.file.Store(nil)
	}
}

// close marks the segment for closure (decrements ref count)
func (s *segment) close() error {
	s.release()
	return nil
}

// Size returns the current size of the segment
func (s *segment) Size() int64 {
	return s.size.Load()
}
