package lsm

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Config contains configuration for the LSM-Tree
type Config struct {
	DataDir      string
	MemTableSize int // Maximum memtable size in bytes
	MaxL0Files   int // Trigger compaction when L0 reaches this many files
}

// DefaultConfig returns a default configuration
func DefaultConfig(dataDir string) Config {
	return Config{
		DataDir:      dataDir,
		MemTableSize: 4 * 1024 * 1024, // 4MB
		MaxL0Files:   4,
	}
}

// LSM is the main LSM-Tree storage engine
type LSM struct {
	config            Config
	mu                sync.RWMutex
	activeMemtable    *MemTable
	immutableMemtable *MemTable
	wal               *WAL
	levels            *LevelManager
	sequence          uint64 // Atomic counter for ordering
	nextFileNum       uint64 // Atomic counter for SSTable numbering

	flushChan      chan struct{}
	compactionChan chan struct{}
	closeChan      chan struct{}
	wg             sync.WaitGroup

	// Stats tracking
	stats struct {
		writeCount   atomic.Int64
		readCount    atomic.Int64
		flushCount   atomic.Int64
		compactCount atomic.Int64
	}
}

// New creates a new LSM-Tree storage engine
func New(config Config) (*LSM, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Open WAL
	walPath := filepath.Join(config.DataDir, "wal.log")
	wal, err := NewWAL(walPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	lsm := &LSM{
		config:         config,
		activeMemtable: NewMemTable(config.MemTableSize),
		levels:         NewLevelManager(),
		wal:            wal,
		flushChan:      make(chan struct{}, 1),
		compactionChan: make(chan struct{}, 1),
		closeChan:      make(chan struct{}),
	}

	// Recover from WAL
	if err := lsm.recoverFromWAL(); err != nil {
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	// Load existing SSTables
	if err := lsm.loadSSTables(); err != nil {
		return nil, fmt.Errorf("failed to load SSTables: %w", err)
	}

	// Start background workers
	lsm.wg.Add(2)
	go lsm.flushWorker()
	go lsm.compactionWorker()

	log.Printf("LSM-Tree initialized at %s", config.DataDir)

	return lsm, nil
}

// Put inserts a key-value pair
func (lsm *LSM) Put(key string, value []byte) error {
	// Get next sequence number
	seq := atomic.AddUint64(&lsm.sequence, 1)

	// Append to WAL
	if err := lsm.wal.Append(key, value, seq, false); err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Insert into active memtable
	lsm.mu.RLock()
	lsm.activeMemtable.Put(key, value, seq)
	isFull := lsm.activeMemtable.IsFull()

	lsm.mu.RUnlock()

	// Track write
	lsm.stats.writeCount.Add(1)

	// Trigger flush if memtable is full
	if isFull {

		lsm.mu.Lock()
		// Double-check after acquiring write lock
		if lsm.activeMemtable.IsFull() && lsm.immutableMemtable == nil {
			// Freeze current memtable
			lsm.immutableMemtable = lsm.activeMemtable
			lsm.activeMemtable = NewMemTable(lsm.config.MemTableSize)

			// Signal flush worker
			select {
			case lsm.flushChan <- struct{}{}:
			default:
			}
		}
		lsm.mu.Unlock()
	}

	return nil
}

// Get retrieves a value for a key
func (lsm *LSM) Get(key string) ([]byte, bool, error) {
	// Track read
	lsm.stats.readCount.Add(1)

	// Check active memtable
	lsm.mu.RLock()
	value, _, deleted, found := lsm.activeMemtable.Get(key)
	if found {
		lsm.mu.RUnlock()
		if deleted {
			return nil, false, nil
		}
		return value, true, nil
	}

	// Check immutable memtable
	if lsm.immutableMemtable != nil {
		value, _, deleted, found := lsm.immutableMemtable.Get(key)
		if found {
			lsm.mu.RUnlock()
			if deleted {
				return nil, false, nil
			}
			return value, true, nil
		}
	}
	lsm.mu.RUnlock()

	// Check SSTables in order (L0, L1, L2, L3, L4)
	for level := 0; level < 5; level++ {
		sstables := lsm.levels.GetAllSSTables(level)

		// For L0, check all files (they may overlap)
		if level == 0 {
			for _, sst := range sstables {
				value, found, err := sst.Get(key)
				if err != nil {
					return nil, false, err
				}
				if found {
					return value, true, nil
				}
			}
		} else {
			// For L1+, use binary search on non-overlapping files
			for _, sst := range sstables {
				if key >= sst.MinKey() && key <= sst.MaxKey() {
					value, found, err := sst.Get(key)
					if err != nil {
						return nil, false, err
					}
					if found {
						return value, true, nil
					}
					break // Non-overlapping, so can stop
				}
			}
		}
	}

	return nil, false, nil
}

// Delete marks a key as deleted
func (lsm *LSM) Delete(key string) error {
	// Get next sequence number
	seq := atomic.AddUint64(&lsm.sequence, 1)

	// Append tombstone to WAL
	if err := lsm.wal.Append(key, nil, seq, true); err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Insert tombstone into active memtable
	lsm.mu.RLock()
	lsm.activeMemtable.Delete(key, seq)
	isFull := lsm.activeMemtable.IsFull()
	lsm.mu.RUnlock()

	// Trigger flush if memtable is full
	if isFull {
		lsm.mu.Lock()
		if lsm.activeMemtable.IsFull() && lsm.immutableMemtable == nil {
			lsm.immutableMemtable = lsm.activeMemtable
			lsm.activeMemtable = NewMemTable(lsm.config.MemTableSize)

			select {
			case lsm.flushChan <- struct{}{}:
			default:
			}
		}
		lsm.mu.Unlock()
	}

	return nil
}

// Sync forces a WAL sync to disk
func (lsm *LSM) Sync() error {
	return lsm.wal.Sync()
}

// Close closes the LSM-Tree and flushes all data
func (lsm *LSM) Close() error {

	// Signal workers to stop
	close(lsm.closeChan)
	lsm.wg.Wait()

	// Flush active memtable if it has data
	lsm.mu.Lock()
	if lsm.activeMemtable.Len() > 0 {
		if err := lsm.flushMemtable(lsm.activeMemtable); err != nil {
			lsm.mu.Unlock()
			return err
		}
	}
	lsm.mu.Unlock()

	// Close WAL
	if err := lsm.wal.Close(); err != nil {
		return err
	}

	// Close all SSTables
	if err := lsm.levels.CloseAll(); err != nil {
		return err
	}

	return nil
}

// recoverFromWAL replays the WAL to restore memtable state
func (lsm *LSM) recoverFromWAL() error {
	entries, err := lsm.wal.ReadAll()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	log.Printf("Recovering %d entries from WAL", len(entries))

	for _, entry := range entries {
		if entry.Sequence > lsm.sequence {
			lsm.sequence = entry.Sequence
		}

		if entry.Deleted {
			lsm.activeMemtable.Delete(entry.Key, entry.Sequence)
		} else {
			lsm.activeMemtable.Put(entry.Key, entry.Value, entry.Sequence)
		}
	}

	return nil
}

// loadSSTables scans the data directory and loads existing SSTables
func (lsm *LSM) loadSSTables() error {
	files, err := os.ReadDir(lsm.config.DataDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".sst" {
			continue
		}

		// Parse filename: L{level}-{filenum}.sst
		var level int
		var fileNum uint64
		_, err := fmt.Sscanf(file.Name(), "L%d-%d.sst", &level, &fileNum)
		if err != nil {
			log.Printf("Warning: skipping malformed SSTable filename: %s", file.Name())
			continue
		}

		// Open SSTable
		path := filepath.Join(lsm.config.DataDir, file.Name())
		sst, err := OpenSSTable(path, level, fileNum)
		if err != nil {
			log.Printf("Warning: failed to open SSTable %s: %v", file.Name(), err)
			continue
		}

		// Add to level manager
		lsm.levels.AddSSTable(sst, level)

		// Track max file number
		if fileNum >= lsm.nextFileNum {
			lsm.nextFileNum = fileNum + 1
		}

	}

	return nil
}

// flushMemtable writes a memtable to disk as an L0 SSTable
func (lsm *LSM) flushMemtable(memtable *MemTable) error {
	entries := memtable.GetAllEntries()
	if len(entries) == 0 {
		return nil
	}

	fileNum := atomic.AddUint64(&lsm.nextFileNum, 1) - 1
	path := filepath.Join(lsm.config.DataDir, fmt.Sprintf("L0-%06d.sst", fileNum))

	// Track flush
	lsm.stats.flushCount.Add(1)

	// Build SSTable
	builder, err := NewSSTableBuilder(path, len(entries))
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := builder.Add(entry.Key, entry.Value, entry.Deleted); err != nil {
			builder.Abort()
			return err
		}
	}

	if err := builder.Finish(); err != nil {
		return err
	}

	// Open the newly created SSTable
	sst, err := OpenSSTable(path, 0, fileNum)
	if err != nil {
		return err
	}

	// Add to L0
	lsm.levels.AddSSTable(sst, 0)

	return nil
}

// flushWorker handles background memtable flushes
func (lsm *LSM) flushWorker() {
	defer lsm.wg.Done()

	for {
		select {
		case <-lsm.closeChan:
			return
		case <-lsm.flushChan:
			lsm.mu.Lock()
			if lsm.immutableMemtable != nil {
				if err := lsm.flushMemtable(lsm.immutableMemtable); err != nil {
					log.Printf("Error flushing memtable: %v", err)
				} else {
					lsm.immutableMemtable = nil

					// Delete WAL (it's been persisted to SSTable)
					lsm.wal.Delete()
					walPath := filepath.Join(lsm.config.DataDir, "wal.log")
					wal, err := NewWAL(walPath)
					if err != nil {
						log.Printf("Error recreating WAL: %v", err)
					} else {
						lsm.wal = wal
					}
				}
			}
			lsm.mu.Unlock()

			// Check if compaction is needed
			if lsm.levels.ShouldCompact(0) {
				select {
				case lsm.compactionChan <- struct{}{}:
				default:
				}
			}
		}
	}
}

// compactionWorker handles background compactions
func (lsm *LSM) compactionWorker() {
	defer lsm.wg.Done()

	for {
		select {
		case <-lsm.closeChan:
			return
		case <-lsm.compactionChan:
			lsm.performCompaction()
		}
	}
}

// performCompaction performs compaction across all levels (L0→L1, L1→L2, L2→L3, L3→L4)
func (lsm *LSM) performCompaction() {
	// Check if L0 needs compaction
	if lsm.levels.ShouldCompact(0) {
		lsm.compactL0ToL1()
		// Trigger next level compaction if needed
		lsm.triggerNextLevelCompaction(1)
		return
	}

	// Check L1→L2, L2→L3, L3→L4 compactions
	for level := 1; level < 4; level++ {
		if lsm.levels.ShouldCompact(level) {
			lsm.compactLevel(level, level+1)
			// Trigger next level compaction if needed
			lsm.triggerNextLevelCompaction(level + 1)
			return
		}
	}
}

// compactL0ToL1 handles L0→L1 compaction (special case for overlapping files)
func (lsm *LSM) compactL0ToL1() {

	lsm.stats.compactCount.Add(1)

	l0Files := lsm.levels.GetAllSSTables(0)
	l1Files := lsm.levels.GetAllSSTables(1)

	newL1Files, oldL1Files, err := CompactL0ToL1(lsm.config.DataDir, l0Files, l1Files, &lsm.nextFileNum)
	if err != nil {
		log.Printf("Error during L0->L1 compaction: %v", err)
		return
	}

	// Update level manager
	lsm.mu.Lock()
	for _, sst := range l0Files {
		lsm.levels.RemoveSSTable(sst, 0)
	}
	for _, sst := range oldL1Files {
		lsm.levels.RemoveSSTable(sst, 1)
	}
	for _, sst := range newL1Files {
		lsm.levels.AddSSTable(sst, 1)
	}
	lsm.mu.Unlock()

	// Delete old files
	DeleteSSTables(l0Files)
	DeleteSSTables(oldL1Files)

}

// compactLevel handles Ln→Ln+1 compaction for levels 1 and above
func (lsm *LSM) compactLevel(sourceLevel, targetLevel int) {

	lsm.stats.compactCount.Add(1)

	sourceFiles := lsm.levels.PickCompactionFiles(sourceLevel)
	targetFiles := lsm.levels.GetAllSSTables(targetLevel)

	newFiles, oldTargetFiles, err := CompactLnToLn1(lsm.config.DataDir, sourceFiles, targetFiles, targetLevel, &lsm.nextFileNum)
	if err != nil {
		log.Printf("Error during L%d->L%d compaction: %v", sourceLevel, targetLevel, err)
		return
	}

	// Update level manager
	lsm.mu.Lock()
	for _, sst := range sourceFiles {
		lsm.levels.RemoveSSTable(sst, sourceLevel)
	}
	for _, sst := range oldTargetFiles {
		lsm.levels.RemoveSSTable(sst, targetLevel)
	}
	for _, sst := range newFiles {
		lsm.levels.AddSSTable(sst, targetLevel)
	}
	lsm.mu.Unlock()

	// Delete old files
	DeleteSSTables(sourceFiles)
	DeleteSSTables(oldTargetFiles)

}

// triggerNextLevelCompaction triggers compaction for the next level if needed
func (lsm *LSM) triggerNextLevelCompaction(level int) {
	if lsm.levels.ShouldCompact(level) {
		select {
		case lsm.compactionChan <- struct{}{}:
		default:
		}
	}
}

// GetLevels returns the level manager (for debugging/stats)
func (lsm *LSM) GetLevels() *LevelManager {
	return lsm.levels
}
