package lsm

import (
	"sort"
	"sync"
)

const (
	maxL0Files = 4                      // Trigger L0->L1 compaction
	l0MaxSize  = 40 * 1024 * 1024       // 40 MB
	l1MaxSize  = 400 * 1024 * 1024      // 400 MB
	l2MaxSize  = 4 * 1024 * 1024 * 1024 // 4 GB
	l3MaxSize  = 40 * 1024 * 1024 * 1024 // 40 GB
	l4MaxSize  = 400 * 1024 * 1024 * 1024 // 400 GB
)

// LevelInfo contains metadata for a single level
type LevelInfo struct {
	sstables []*SSTable
	size     int64
	maxSize  int64
}

// LevelManager manages SSTables across multiple levels
type LevelManager struct {
	mu     sync.RWMutex
	levels []LevelInfo
}

// NewLevelManager creates a new level manager with 5 levels (L0, L1, L2, L3, L4)
func NewLevelManager() *LevelManager {
	return &LevelManager{
		levels: []LevelInfo{
			{sstables: make([]*SSTable, 0), maxSize: l0MaxSize},  // L0
			{sstables: make([]*SSTable, 0), maxSize: l1MaxSize},  // L1
			{sstables: make([]*SSTable, 0), maxSize: l2MaxSize},  // L2
			{sstables: make([]*SSTable, 0), maxSize: l3MaxSize},  // L3
			{sstables: make([]*SSTable, 0), maxSize: l4MaxSize},  // L4
		},
	}
}

// AddSSTable adds an SSTable to a level
// For L1+, sorts by key range to maintain non-overlapping property
func (lm *LevelManager) AddSSTable(sst *SSTable, level int) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if level >= len(lm.levels) {
		return
	}

	lm.levels[level].sstables = append(lm.levels[level].sstables, sst)

	// Sort by minimum key for L1+ (maintains non-overlapping order)
	if level > 0 {
		sort.Slice(lm.levels[level].sstables, func(i, j int) bool {
			return lm.levels[level].sstables[i].MinKey() < lm.levels[level].sstables[j].MinKey()
		})
	}

	// Update size (approximate)
	lm.updateLevelSize(level)
}

// RemoveSSTable removes an SSTable from a level
func (lm *LevelManager) RemoveSSTable(sst *SSTable, level int) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if level >= len(lm.levels) {
		return
	}

	// Find and remove the SSTable
	sstables := lm.levels[level].sstables
	for i, s := range sstables {
		if s.FileNum() == sst.FileNum() {
			lm.levels[level].sstables = append(sstables[:i], sstables[i+1:]...)
			break
		}
	}

	lm.updateLevelSize(level)
}

// GetOverlapping returns SSTables in a level that overlap with [start, end]
// For L0, may return multiple overlapping files
// For L1+, returns files whose key ranges overlap
func (lm *LevelManager) GetOverlapping(level int, start, end string) []*SSTable {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return nil
	}

	var overlapping []*SSTable
	for _, sst := range lm.levels[level].sstables {
		if sst.Overlaps(start, end) {
			overlapping = append(overlapping, sst)
		}
	}

	return overlapping
}

// GetAllSSTables returns all SSTables in a level
func (lm *LevelManager) GetAllSSTables(level int) []*SSTable {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return nil
	}

	// Return a copy to avoid race conditions
	sstables := make([]*SSTable, len(lm.levels[level].sstables))
	copy(sstables, lm.levels[level].sstables)
	return sstables
}

// ShouldCompact checks if a level needs compaction
func (lm *LevelManager) ShouldCompact(level int) bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return false
	}

	// L0 uses file count instead of size
	if level == 0 {
		return len(lm.levels[0].sstables) >= maxL0Files
	}

	// L1+ uses size threshold
	return lm.levels[level].size >= lm.levels[level].maxSize
}

// NumFiles returns the number of SSTables in a level
func (lm *LevelManager) NumFiles(level int) int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return 0
	}

	return len(lm.levels[level].sstables)
}

// LevelSize returns the total size of a level
func (lm *LevelManager) LevelSize(level int) int64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return 0
	}

	return lm.levels[level].size
}

// updateLevelSize recalculates the total size of a level
// Must be called with lock held
func (lm *LevelManager) updateLevelSize(level int) {
	// Approximate size based on number of files and average block count
	// In a real implementation, we'd track actual file sizes
	lm.levels[level].size = int64(len(lm.levels[level].sstables)) * 4 * 1024 * 1024 // Assume ~4MB per file
}

// CloseAll closes all SSTables
func (lm *LevelManager) CloseAll() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for _, level := range lm.levels {
		for _, sst := range level.sstables {
			if err := sst.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

// PickCompactionFiles selects files for compaction at a given level
// For L0: returns all files (they may overlap)
// For L1+: returns 1-2 files that should be compacted
func (lm *LevelManager) PickCompactionFiles(level int) []*SSTable {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if level >= len(lm.levels) {
		return nil
	}

	// L0: return all files for compaction to L1
	if level == 0 {
		files := make([]*SSTable, len(lm.levels[0].sstables))
		copy(files, lm.levels[0].sstables)
		return files
	}

	// L1+: pick oldest file (simple strategy)
	if len(lm.levels[level].sstables) > 0 {
		return []*SSTable{lm.levels[level].sstables[0]}
	}

	return nil
}

// GetTotalFiles returns the total number of SSTables across all levels
func (lm *LevelManager) GetTotalFiles() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	total := 0
	for _, level := range lm.levels {
		total += len(level.sstables)
	}
	return total
}

// GetTotalSize returns the total size across all levels
func (lm *LevelManager) GetTotalSize() int64 {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var total int64
	for _, level := range lm.levels {
		total += level.size
	}
	return total
}
