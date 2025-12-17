package lsm

import "github.com/intellect4all/storage-engines/common"

// Adapter wraps LSM to implement common.StorageEngine interface
// LSM uses string keys, but the interface expects []byte keys
type Adapter struct {
	lsm *LSM
}

// NewAdapter creates a new adapter for LSM
func NewAdapter(config Config) (*Adapter, error) {
	lsm, err := New(config)
	if err != nil {
		return nil, err
	}
	return &Adapter{lsm: lsm}, nil
}

// Put implements common.StorageEngine
func (a *Adapter) Put(key, value []byte) error {
	return a.lsm.Put(string(key), value)
}

// Get implements common.StorageEngine
func (a *Adapter) Get(key []byte) ([]byte, error) {
	value, found, err := a.lsm.Get(string(key))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, common.ErrKeyNotFound
	}
	return value, nil
}

// Delete implements common.StorageEngine
func (a *Adapter) Delete(key []byte) error {
	return a.lsm.Delete(string(key))
}

// Close implements common.StorageEngine
func (a *Adapter) Close() error {
	return a.lsm.Close()
}

// Sync implements common.StorageEngine
func (a *Adapter) Sync() error {
	return a.lsm.Sync()
}

// Stats implements common.StorageEngine
func (a *Adapter) Stats() common.Stats {
	// Calculate statistics
	totalFiles := a.lsm.levels.GetTotalFiles()
	totalSize := a.lsm.levels.GetTotalSize()

	// Active segment size is the memtable size
	activeSegSize := int64(a.lsm.activeMemtable.Size())

	// Get tracked stats
	writeCount := a.lsm.stats.writeCount.Load()
	readCount := a.lsm.stats.readCount.Load()
	compactCount := a.lsm.stats.compactCount.Load()
	flushCount := a.lsm.stats.flushCount.Load()

	// Calculate approximate number of keys
	// Count unique keys in active + immutable memtables, plus estimate from SSTables
	numKeys := int64(a.lsm.activeMemtable.Len())
	if a.lsm.immutableMemtable != nil {
		numKeys += int64(a.lsm.immutableMemtable.Len())
	}
	// Estimate from SSTables (rough approximation: 10k keys per file)
	numKeys += int64(totalFiles * 10000)

	// Calculate write amplification (bytes written to disk / bytes written by user)
	// For LSM-Tree: Each flush rewrites data once, each compaction merges multiple files
	// Simple estimate: count flushes and compactions relative to total L0 files
	// Typical LSM write amp: 2-4x for write-heavy workloads
	writeAmp := 1.0
	if flushCount > 0 {
		// Base amp from flushes (data written to L0)
		writeAmp = 1.5

		// Add compaction overhead
		if compactCount > 0 && flushCount > 0 {
			// Each compaction roughly doubles the I/O
			compactionRatio := float64(compactCount) / float64(flushCount)
			writeAmp += compactionRatio * 0.5
		}

		// Cap at reasonable values for LSM
		if writeAmp > 5.0 {
			writeAmp = 5.0
		}
	}

	// Calculate space amplification (disk space / logical data)
	// For LSM: space amp depends on how much duplicate/stale data exists
	// Typically 1.2-1.5x with good compaction, can go higher with compaction lag
	spaceAmp := 1.0
	if totalFiles > 0 {
		// Estimate based on number of levels and files
		// More files at L0 = more duplicates = higher space amp
		l0Files := a.lsm.levels.NumFiles(0)
		if l0Files > 2 {
			// High compaction lag
			spaceAmp = 1.5 + float64(l0Files)*0.1
		} else {
			// Good compaction
			spaceAmp = 1.2
		}

		// Cap at reasonable value
		if spaceAmp > 3.0 {
			spaceAmp = 3.0
		}
	}

	return common.Stats{
		NumKeys:       numKeys,
		NumSegments:   totalFiles + 1, // +1 for active memtable
		ActiveSegSize: activeSegSize,
		TotalDiskSize: totalSize,
		WriteCount:    writeCount,
		ReadCount:     readCount,
		CompactCount:  compactCount,
		WriteAmp:      writeAmp,
		SpaceAmp:      spaceAmp,
	}
}

// Compact implements common.StorageEngine
func (a *Adapter) Compact() error {
	// LSM has automatic compaction, this is a no-op
	// Could trigger a manual compaction if implemented
	return nil
}

// Scan returns an iterator for range queries (LSM-specific feature)
func (a *Adapter) Scan(start, end string) Iterator {
	return a.lsm.Scan(start, end)
}
