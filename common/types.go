package common

// StorageEngine is the interface all storage engines implement
type StorageEngine interface {
	Put(key, value []byte) error

	// Get Returns ErrKeyNotFound if key doesn't exist
	Get(key []byte) ([]byte, error)

	// Delete removes a key
	Delete(key []byte) error

	// Close closes the storage engine
	Close() error

	// Sync ensures all data is persisted to disk
	Sync() error

	// Stats returns engine statistics
	Stats() Stats

	// Compact manually triggers compaction
	Compact() error
}

// Stats contains engine statistics
type Stats struct {
	// Basic counts
	NumKeys       int64
	NumSegments   int
	ActiveSegSize int64
	TotalDiskSize int64

	// Performance metrics
	WriteCount   int64
	ReadCount    int64
	CompactCount int64

	// Amplification factors
	WriteAmp float64 // bytes written to disk / bytes written by user
	SpaceAmp float64 // disk space used / logical data size
}

// Iterator for range scans
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Error() error
	Close() error
}
