package benchmark

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/intellect4all/storage-engines/common"
)

// WorkloadType defines the access pattern
type WorkloadType string

const (
	WorkloadWriteHeavy WorkloadType = "write-heavy" // 95% writes
	WorkloadReadHeavy  WorkloadType = "read-heavy"  // 95% reads
	WorkloadBalanced   WorkloadType = "balanced"    // 50/50
	WorkloadReadOnly   WorkloadType = "read-only"   // 100% reads
	WorkloadWriteOnly  WorkloadType = "write-only"  // 100% writes
)

// Config defines a benchmark scenario
type Config struct {
	Name string

	WorkloadType    WorkloadType
	KeyDistribution KeyDistribution

	NumKeys   int // Total unique keys in dataset
	KeySize   int // Bytes
	ValueSize int // Bytes

	Duration    time.Duration // How long to run
	Concurrency int           // Number of concurrent workers

	PreloadKeys int // Keys to load before benchmark starts

	Seed int64
}

type Result struct {
	Config Config

	// Throughput
	TotalOps  int64
	WriteOps  int64
	ReadOps   int64
	Duration  time.Duration
	OpsPerSec float64

	// Latency (microseconds)
	WriteLatency LatencyStats
	ReadLatency  LatencyStats

	// Amplification
	WriteAmplification float64 // Measured from engine stats
	ReadAmplification  float64
	SpaceAmplification float64

	// Resource usage
	PeakMemoryMB float64
	TotalDiskMB  float64

	// Engine-specific stats
	EngineStats common.Stats
}

type Benchmark struct {
	engine common.StorageEngine
	config Config

	// Metrics collection
	writeLatencies *LatencyHistogram
	readLatencies  *LatencyHistogram

	// Counters
	writeCount atomic.Int64
	readCount  atomic.Int64
	errorCount atomic.Int64

	// Key generation
	keyGen *KeyGenerator

	// Random seed for this benchmark
	randSeed atomic.Int64
}

func NewBenchmark(engine common.StorageEngine, config Config) *Benchmark {
	return &Benchmark{
		engine:         engine,
		config:         config,
		writeLatencies: NewLatencyHistogram(),
		readLatencies:  NewLatencyHistogram(),
		keyGen:         NewKeyGenerator(config.NumKeys, config.KeySize, config.KeyDistribution, config.Seed),
	}
}

// Run executes the benchmark
func (b *Benchmark) Run() (*Result, error) {
	// Phase 1: Preload data
	if b.config.PreloadKeys > 0 {
		fmt.Printf("Preloading %d keys...\n", b.config.PreloadKeys)
		if err := b.preload(); err != nil {
			return nil, err
		}
		fmt.Println("Preload complete")
	}

	// Phase 2: Warm-up (not measured)
	fmt.Println("Warming up...")
	b.runWorkload(5 * time.Second)

	// Reset metrics
	b.writeLatencies = NewLatencyHistogram()
	b.readLatencies = NewLatencyHistogram()
	b.writeCount.Store(0)
	b.readCount.Store(0)
	b.errorCount.Store(0)

	// Phase 3: Actual benchmark
	fmt.Printf("Running benchmark for %v...\n", b.config.Duration)
	startStats := b.engine.Stats()
	startTime := time.Now()

	b.runWorkload(b.config.Duration)

	endTime := time.Now()
	endStats := b.engine.Stats()
	duration := endTime.Sub(startTime)

	// Phase 4: Calculate results
	result := b.calculateResults(duration, startStats, endStats)

	return result, nil
}

// preload fills the database with initial data
func (b *Benchmark) preload() error {
	value := make([]byte, b.config.ValueSize)
	rand.Read(value)

	for i := 0; i < b.config.PreloadKeys; i++ {
		key := b.keyGen.GenerateSequential(i)
		if err := b.engine.Put(key, value); err != nil {
			return err
		}

		if i > 0 && i%10000 == 0 {
			fmt.Printf("  Loaded %d keys\n", i)
		}
	}

	// Force sync after preload
	return b.engine.Sync()
}

// runWorkload executes the workload for the given duration
func (b *Benchmark) runWorkload(duration time.Duration) {
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Start workers
	for i := 0; i < b.config.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			b.worker(workerID, stop)
		}(i)
	}

	// Let them run
	time.Sleep(duration)

	// Stop workers
	close(stop)
	wg.Wait()
}

// worker performs operations until stopped
func (b *Benchmark) worker(id int, stop <-chan struct{}) {
	value := make([]byte, b.config.ValueSize)
	rand.Read(value)

	for {
		select {
		case <-stop:
			return
		default:
			if b.shouldWrite() {
				b.doWrite(value)
			} else {
				b.doRead()
			}
		}
	}
}

// shouldWrite determines if this operation should be a write
func (b *Benchmark) shouldWrite() bool {
	switch b.config.WorkloadType {
	case WorkloadWriteOnly:
		return true
	case WorkloadReadOnly:
		return false
	case WorkloadWriteHeavy:
		return b.randFloat() < 0.95
	case WorkloadReadHeavy:
		return b.randFloat() < 0.05
	case WorkloadBalanced:
		return b.randFloat() < 0.50
	default:
		return b.randFloat() < 0.50
	}
}

func (b *Benchmark) doWrite(value []byte) {
	key := b.keyGen.NextKey()

	start := time.Now()
	err := b.engine.Put(key, value)
	latency := time.Since(start)

	if err != nil {
		b.errorCount.Add(1)
		return
	}

	b.writeLatencies.Record(latency)
	b.writeCount.Add(1)
}

func (b *Benchmark) doRead() {
	key := b.keyGen.NextKey()

	start := time.Now()
	_, err := b.engine.Get(key)
	latency := time.Since(start)

	if err != nil && !errors.Is(err, common.ErrKeyNotFound) {
		b.errorCount.Add(1)
		return
	}

	b.readLatencies.Record(latency)
	b.readCount.Add(1)
}

func (b *Benchmark) calculateResults(duration time.Duration, startStats, endStats common.Stats) *Result {
	writeOps := b.writeCount.Load()
	readOps := b.readCount.Load()
	totalOps := writeOps + readOps

	result := &Result{
		Config:    b.config,
		TotalOps:  totalOps,
		WriteOps:  writeOps,
		ReadOps:   readOps,
		Duration:  duration,
		OpsPerSec: float64(totalOps) / duration.Seconds(),

		WriteLatency: b.writeLatencies.Stats(),
		ReadLatency:  b.readLatencies.Stats(),

		// Amplification from engine stats
		WriteAmplification: endStats.WriteAmp,
		SpaceAmplification: endStats.SpaceAmp,

		TotalDiskMB: float64(endStats.TotalDiskSize) / (1024 * 1024),
		EngineStats: endStats,
	}

	return result
}

func (b *Benchmark) randFloat() float64 {
	return float64(b.randSeed.Add(1)%10000) / 10000.0
}
