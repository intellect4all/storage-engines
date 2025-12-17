package hashindex

import (
	"fmt"
	"io"
	"os"
	"time"
)

// compactSegments compacts multiple segments into one
// Returns: new segment, new index entries, error
func (h *HashIndex) compactSegments(segments []*segment) (*segment, map[string]*indexEntry, error) {

	latestValues := make(map[string][]byte)

	for _, seg := range segments {
		offset := int64(0)
		segSize := seg.Size()

		for offset < segSize {
			key, value, nextOffset, err := seg.readRecord(offset)
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, nil, fmt.Errorf("error reading segment %d: %w", seg.id, err)
			}

			latestValues[string(key)] = value
			offset = nextOffset
		}
	}

	// Create new compacted segment
	newSeg, err := h.createSegment()
	if err != nil {
		return nil, nil, err
	}

	// Write all latest values to new segment
	newIndex := make(map[string]*indexEntry)
	compactionBytesWritten := int64(0)

	for key, value := range latestValues {
		// Skip tombstones (deleted keys have nil or empty value)
		if value == nil || len(value) == 0 {
			continue
		}

		offset, size, err := newSeg.append([]byte(key), value)
		if err != nil {
			newSeg.close()
			os.Remove(newSeg.path)
			return nil, nil, err
		}

		newIndex[key] = &indexEntry{
			segmentID: newSeg.id,
			offset:    offset,
			size:      size,
			timestamp: time.Now().Unix(),
		}

		compactionBytesWritten += int64(size)
	}

	h.stats.bytesWrittenToDisk.Add(compactionBytesWritten)

	if err := newSeg.sync(); err != nil {
		newSeg.close()
		os.Remove(newSeg.path)
		return nil, nil, err
	}

	return newSeg, newIndex, nil
}

func (h *HashIndex) applyCompaction(oldSegments []*segment, newSegment *segment, newIndex map[string]*indexEntry) error {

	compactedIDs := make(map[int]bool)
	for _, seg := range oldSegments {
		compactedIDs[seg.id] = true
	}

	// Prepare batch updates
	updates := make(map[string]*indexEntry)
	deletions := make([]string, 0)

	for shardIdx := 0; shardIdx < 256; shardIdx++ {
		shard := h.index.shards[shardIdx]

		shard.mu.RLock()
		for key, entry := range shard.entries {
			if compactedIDs[entry.segmentID] {
				if newEntry, exists := newIndex[key]; exists {
					updates[key] = newEntry
				} else {
					deletions = append(deletions, key)
				}
			}
		}
		shard.mu.RUnlock()
	}

	h.index.UpdateBatch(updates, deletions)

	// Update segment list (copy-on-write)
	h.segmentsMu.Lock()
	oldSegmentList := h.segments.Load()

	newSegmentList := make([]*segment, 0, len(*oldSegmentList)+1)
	for _, seg := range *oldSegmentList {
		if !compactedIDs[seg.id] {
			newSegmentList = append(newSegmentList, seg)
		}
	}

	newSegmentList = append(newSegmentList, newSegment)
	h.segments.Store(&newSegmentList)
	h.segmentsMu.Unlock()

	for _, seg := range oldSegments {

		os.Remove(seg.path)

		seg.close()
	}

	h.stats.compactCount.Add(1)

	return nil
}
