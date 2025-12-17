package hashindex

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (h *HashIndex) recover() error {
	// List all segment files
	files, err := os.ReadDir(h.config.DataDir)
	if err != nil {
		return err
	}

	// Parse segment IDs and sort by timestamp (oldest first)
	type segmentInfo struct {
		id   int
		path string
	}
	segmentInfos := make([]segmentInfo, 0)

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".seg") {
			continue
		}

		// Parse segment ID from filename (e.g., "1234567890.seg")
		idStr := strings.TrimSuffix(file.Name(), ".seg")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue // Skip invalid files
		}

		path := filepath.Join(h.config.DataDir, file.Name())
		segmentInfos = append(segmentInfos, segmentInfo{id: id, path: path})
	}

	// Sort by ID (timestamp order)
	sort.Slice(segmentInfos, func(i, j int) bool {
		return segmentInfos[i].id < segmentInfos[j].id
	})

	if len(segmentInfos) == 0 {
		// No segments to recover, will create new one
		return nil
	}

	// Recover all segments
	recoveredSegments := make([]*segment, 0)
	latestValues := make(map[string]*indexEntry)

	for _, info := range segmentInfos {
		file, err := os.OpenFile(info.path, os.O_RDWR, 0644)
		if err != nil {
			return fmt.Errorf("failed to open segment %s: %w", info.path, err)
		}

		// Get file size
		stat, err := file.Stat()
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to stat segment %s: %w", info.path, err)
		}

		seg := newSegment(info.id, info.path, file)
		seg.size.Store(stat.Size())

		// Scan segment and build index
		offset := int64(0)
		for offset < stat.Size() {
			key, _, nextOffset, err := seg.readRecord(offset)
			if err != nil {
				if err == io.EOF {
					break
				}
				// Corruption detected, truncate at this point
				fmt.Printf("Warning: corruption in segment %d at offset %d, truncating\n", seg.id, offset)
				if err := file.Truncate(offset); err != nil {
					fmt.Printf("Failed to truncate segment %d: %v\n", seg.id, err)
				}
				seg.size.Store(offset)
				break
			}

			// Store latest value for this key
			recordSize := int32(nextOffset - offset)
			latestValues[string(key)] = &indexEntry{
				segmentID: seg.id,
				offset:    offset,
				size:      recordSize,
				timestamp: time.Now().Unix(),
			}

			offset = nextOffset
		}

		recoveredSegments = append(recoveredSegments, seg)
	}

	// The last segment becomes the active segment
	if len(recoveredSegments) > 0 {
		activeSeg := recoveredSegments[len(recoveredSegments)-1]
		h.activeSegment.Store(activeSeg)

		// All others are immutable
		if len(recoveredSegments) > 1 {
			immutableSegs := recoveredSegments[:len(recoveredSegments)-1]
			h.segments.Store(&immutableSegs)
		}
	}

	// Rebuild index from latest values
	for key, entry := range latestValues {
		// Skip tombstones
		if entry.size == 0 {
			continue
		}
		h.index.Put(key, entry)
	}

	fmt.Printf("Recovered %d segments, %d keys\n", len(recoveredSegments), h.index.Count())

	return nil
}
