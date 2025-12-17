package hashindex

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/intellect4all/storage-engines/common"
)

type Config struct {
	DataDir          string
	SegmentSizeBytes int64 // Rotate to new segment when this size reached
	MaxSegments      int   // Trigger compaction when this many segments exist
	SyncOnWrite      bool  // fsync after every write (slow but durable)
}

func DefaultConfig(dataDir string) Config {
	return Config{
		DataDir:          dataDir,
		SegmentSizeBytes: 4 * 1024 * 1024,
		MaxSegments:      4,
		SyncOnWrite:      false,
	}
}

// HashIndex is a high-concurrency hash index with lock-free techniques
type HashIndex struct {
	config Config

	index *shardedIndex

	activeSegment atomic.Pointer[segment]
	segmentMu     sync.Mutex

	segments   atomic.Pointer[[]*segment]
	segmentsMu sync.Mutex

	compactChan chan struct{}
	compactWg   sync.WaitGroup
	stopChan    chan struct{}

	stats struct {
		writeCount         atomic.Int64
		readCount          atomic.Int64
		compactCount       atomic.Int64
		bytesWritten       atomic.Int64
		bytesWrittenToDisk atomic.Int64
		bytesRead          atomic.Int64
	}

	closed  atomic.Bool
	closeMu sync.Mutex
}

func New(config Config) (*HashIndex, error) {
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, err
	}

	h := &HashIndex{
		config:      config,
		index:       newShardedIndex(),
		compactChan: make(chan struct{}, 1),
		stopChan:    make(chan struct{}),
	}

	emptySegments := make([]*segment, 0)
	h.segments.Store(&emptySegments)

	if err := h.recover(); err != nil {
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	if h.activeSegment.Load() == nil {
		seg, err := h.createSegment()
		if err != nil {
			return nil, err
		}
		h.activeSegment.Store(seg)
	}

	h.compactWg.Add(1)
	go h.compactionWorker()

	return h, nil
}

func (h *HashIndex) Put(key, value []byte) error {
	if len(key) == 0 {
		return common.ErrKeyEmpty
	}

	if h.closed.Load() {
		return common.ErrClosed
	}

	activeSeg := h.activeSegment.Load()
	if activeSeg != nil && activeSeg.Size() < h.config.SegmentSizeBytes {
		offset, recordSize, err := activeSeg.append(key, value)
		if err == nil {
			h.index.Put(string(key), &indexEntry{
				segmentID: activeSeg.id,
				offset:    offset,
				size:      recordSize,
				timestamp: time.Now().Unix(),
			})

			h.stats.writeCount.Add(1)
			h.stats.bytesWritten.Add(int64(len(key) + len(value)))
			h.stats.bytesWrittenToDisk.Add(int64(recordSize)) // Track actual disk write

			if h.config.SyncOnWrite {
				activeSeg.sync()
			}

			return nil
		}

	}

	return h.putWithRotation(key, value)
}

func (h *HashIndex) putWithRotation(key, value []byte) error {
	h.segmentMu.Lock()
	defer h.segmentMu.Unlock()

	// Check again after acquiring lock (another goroutine may have rotated)
	activeSeg := h.activeSegment.Load()
	if activeSeg != nil && activeSeg.Size() < h.config.SegmentSizeBytes {
		offset, recordSize, err := activeSeg.append(key, value)
		if err != nil {
			return err
		}

		h.index.Put(string(key), &indexEntry{
			segmentID: activeSeg.id,
			offset:    offset,
			size:      recordSize,
			timestamp: time.Now().Unix(),
		})

		h.stats.writeCount.Add(1)
		h.stats.bytesWritten.Add(int64(len(key) + len(value)))
		h.stats.bytesWrittenToDisk.Add(int64(recordSize)) // Track actual disk write

		if h.config.SyncOnWrite {
			activeSeg.sync()
		}

		return nil
	}

	// Need to rotate
	if err := h.rotateSegment(); err != nil {
		return err
	}

	// Now write to new active segment
	activeSeg = h.activeSegment.Load()
	offset, recordSize, err := activeSeg.append(key, value)
	if err != nil {
		return err
	}

	h.index.Put(string(key), &indexEntry{
		segmentID: activeSeg.id,
		offset:    offset,
		size:      recordSize,
		timestamp: time.Now().Unix(),
	})

	h.stats.writeCount.Add(1)
	h.stats.bytesWritten.Add(int64(len(key) + len(value)))
	h.stats.bytesWrittenToDisk.Add(int64(recordSize)) // Track actual disk write

	if h.config.SyncOnWrite {
		activeSeg.sync()
	}

	segments := h.segments.Load()
	shouldCompact := false

	if len(*segments) >= h.config.MaxSegments {
		shouldCompact = true
	} else if len(*segments) >= 2 {
		// Also trigger if space amplification is getting high
		// This prevents accumulation of duplicate data
		logicalSize := h.getLogicalSize()
		diskSize := int64(0)
		for _, seg := range *segments {
			diskSize += seg.Size()
		}
		activeSegSize := h.activeSegment.Load().Size()
		diskSize += activeSegSize

		// Trigger compaction if space amp > 3.0x (tunable threshold)
		if logicalSize > 0 && float64(diskSize)/float64(logicalSize) > 3.0 {
			shouldCompact = true
		}
	}

	if shouldCompact {
		select {
		case h.compactChan <- struct{}{}:
		default:
		}
	}

	return nil
}

func (h *HashIndex) Get(key []byte) ([]byte, error) {
	if h.closed.Load() {
		return nil, common.ErrClosed
	}

	entry, exists := h.index.Get(string(key))
	if !exists {
		return nil, common.ErrKeyNotFound
	}

	activeSeg := h.activeSegment.Load()
	var seg *segment

	if entry.segmentID == activeSeg.id {
		seg = activeSeg
	} else {
		segments := h.segments.Load()
		for _, s := range *segments {
			if s.id == entry.segmentID {
				seg = s
				break
			}
		}
	}

	if seg == nil {
		return nil, fmt.Errorf("segment %d not found", entry.segmentID)
	}

	value, err := seg.read(entry.offset)
	if err != nil {
		return nil, err
	}

	// Check for tombstone
	if value == nil || len(value) == 0 {
		return nil, common.ErrKeyNotFound
	}

	h.stats.readCount.Add(1)
	h.stats.bytesRead.Add(int64(len(value)))

	return value, nil
}

func (h *HashIndex) Delete(key []byte) error {
	return h.Put(key, nil)
}

func (h *HashIndex) Close() error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()

	if h.closed.Swap(true) {
		return nil // Already closed
	}

	// Stop background worker
	close(h.stopChan)
	h.compactWg.Wait()

	// Close active segment
	activeSeg := h.activeSegment.Load()
	if activeSeg != nil {
		activeSeg.close()
	}

	// Close all segments
	segments := h.segments.Load()
	for _, seg := range *segments {
		seg.close()
	}

	return nil
}

func (h *HashIndex) Sync() error {
	if h.closed.Load() {
		return common.ErrClosed
	}

	activeSeg := h.activeSegment.Load()
	if activeSeg != nil {
		return activeSeg.sync()
	}

	return nil
}

func (h *HashIndex) Stats() common.Stats {
	numKeys := h.index.Count()

	activeSeg := h.activeSegment.Load()
	activeSegSize := int64(0)
	if activeSeg != nil {
		activeSegSize = activeSeg.Size()
	}

	segments := h.segments.Load()
	numSegments := len(*segments) + 1 // +1 for active

	// Calculate disk size
	totalDiskSize := activeSegSize
	for _, seg := range *segments {
		totalDiskSize += seg.Size()
	}

	writeCount := h.stats.writeCount.Load()
	readCount := h.stats.readCount.Load()
	compactCount := h.stats.compactCount.Load()
	bytesWritten := h.stats.bytesWritten.Load()
	bytesWrittenToDisk := h.stats.bytesWrittenToDisk.Load()

	logicalSize := h.getLogicalSize()

	// Space Amplification: ratio of disk usage to logical data size
	spaceAmp := float64(1.0)
	if logicalSize > 0 {
		spaceAmp = float64(totalDiskSize) / float64(logicalSize)
	}

	// Write Amplification: ratio of bytes written to disk vs. bytes written by user
	// This includes compaction overhead
	writeAmp := float64(1.0)
	if bytesWritten > 0 {
		writeAmp = float64(bytesWrittenToDisk) / float64(bytesWritten)
	}

	return common.Stats{
		NumKeys:       numKeys,
		NumSegments:   numSegments,
		ActiveSegSize: activeSegSize,
		TotalDiskSize: totalDiskSize,
		WriteCount:    writeCount,
		ReadCount:     readCount,
		CompactCount:  compactCount,
		WriteAmp:      writeAmp,
		SpaceAmp:      spaceAmp,
	}
}

// getLogicalSize calculates the total size of all unique keys (latest versions only)
// This represents the actual data size without duplicates or tombstones
func (h *HashIndex) getLogicalSize() int64 {
	var totalSize atomic.Int64

	var wg sync.WaitGroup
	wg.Add(len(h.index.shards))

	// Calculate size for each shard in parallel
	for _, sh := range h.index.shards {
		go func(s *shard) {
			defer wg.Done()

			var shardSize int64
			s.mu.RLock()
			for _, entry := range s.entries {
				// entry.size includes header, so subtract it to get key+value size
				shardSize += int64(entry.size - headerSize)
			}
			s.mu.RUnlock()

			totalSize.Add(shardSize)
		}(sh)
	}

	wg.Wait()
	return totalSize.Load()
}

func (h *HashIndex) Compact() error {
	if h.closed.Load() {
		return common.ErrClosed
	}

	select {
	case h.compactChan <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("compaction already in progress")
	}
}

func (h *HashIndex) rotateSegment() error {
	activeSeg := h.activeSegment.Load()
	if activeSeg == nil {
		return fmt.Errorf("no active segment")
	}

	if err := activeSeg.sync(); err != nil {
		return err
	}

	h.segmentsMu.Lock()
	oldSegments := h.segments.Load()
	newSegments := make([]*segment, len(*oldSegments)+1)
	copy(newSegments, *oldSegments)
	newSegments[len(*oldSegments)] = activeSeg
	h.segments.Store(&newSegments)
	h.segmentsMu.Unlock()

	// Create new active segment
	newSeg, err := h.createSegment()
	if err != nil {
		return err
	}

	h.activeSegment.Store(newSeg)
	return nil
}

func (h *HashIndex) createSegment() (*segment, error) {
	segmentID := int(time.Now().UnixNano())
	path := filepath.Join(h.config.DataDir, fmt.Sprintf("%d.seg", segmentID))

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	return newSegment(segmentID, path, file), nil
}

func (h *HashIndex) compactionWorker() {
	defer h.compactWg.Done()

	for {
		select {
		case <-h.stopChan:
			return
		case <-h.compactChan:

			if err := h.doCompact(); err != nil {
				fmt.Printf("compaction error: %v\n", err)
			}
		}
	}
}

// doCompact performs the actual compaction using a leveled strategy
// Instead of compacting ALL segments, we compact only a subset to reduce write amplification
func (h *HashIndex) doCompact() error {

	h.segmentsMu.Lock()
	segments := h.segments.Load()
	if len(*segments) < 2 {
		h.segmentsMu.Unlock()
		return nil // Nothing to compact
	}

	numToCompact := len(*segments)

	// If we have many segments, only compact a portion
	// This implements a simple leveled approach
	if numToCompact > 3 {
		// Compact oldest half, but at least 2 segments
		numToCompact = (numToCompact + 1) / 2
		if numToCompact < 2 {
			numToCompact = 2
		}
	}

	// Segments are ordered from oldest to newest (added in order)
	// Select the oldest numToCompact segments
	segmentsToCompact := make([]*segment, numToCompact)
	copy(segmentsToCompact, (*segments)[:numToCompact])

	// Acquire references to prevent deletion during compaction
	for _, seg := range segmentsToCompact {
		if !seg.acquire() {
			h.segmentsMu.Unlock()
			return fmt.Errorf("failed to acquire segment %d", seg.id)
		}
	}
	h.segmentsMu.Unlock()

	// Release references when done
	defer func() {
		for _, seg := range segmentsToCompact {
			seg.release()
		}
	}()

	// Perform compaction (without holding any locks)
	newSeg, newIndex, err := h.compactSegments(segmentsToCompact)
	if err != nil {
		return err
	}

	// Atomically update state
	return h.applyCompaction(segmentsToCompact, newSeg, newIndex)
}
