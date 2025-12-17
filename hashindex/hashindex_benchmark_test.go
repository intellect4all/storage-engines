package hashindex

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/intellect4all/storage-engines/common/benchmark"
)

// TestQuickBenchmark runs a quick performance benchmark
func TestQuickBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark in short mode")
	}

	dir, err := os.MkdirTemp("", "hashindex-bench-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	config.SegmentSizeBytes = 64 * 1024 * 1024
	config.SyncOnWrite = false

	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Run quick workload
	benchConfig := benchmark.Config{
		Name:            "hashindex-quick",
		WorkloadType:    benchmark.WorkloadBalanced,
		KeyDistribution: benchmark.DistUniform,
		NumKeys:         100000,
		KeySize:         16,
		ValueSize:       100,
		Duration:        10 * time.Second,
		Concurrency:     8,
		PreloadKeys:     10000,
		Seed:            12345,
	}

	bench := benchmark.NewBenchmark(h, benchConfig)
	result, err := bench.Run()
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("\n=== HashIndex Quick Benchmark ===\n")
	fmt.Printf("Throughput: %.0f ops/sec\n", result.OpsPerSec)
	fmt.Printf("Total Ops: %d (writes: %d, reads: %d)\n",
		result.TotalOps, result.WriteOps, result.ReadOps)

	if result.WriteOps > 0 {
		fmt.Printf("Write Latency: P50=%v, P99=%v, P999=%v\n",
			result.WriteLatency.P50, result.WriteLatency.P99, result.WriteLatency.P999)
	}

	if result.ReadOps > 0 {
		fmt.Printf("Read Latency: P50=%v, P99=%v, P999=%v\n",
			result.ReadLatency.P50, result.ReadLatency.P99, result.ReadLatency.P999)
	}

	fmt.Printf("Write Amp: %.2fx, Space Amp: %.2fx\n",
		result.WriteAmplification, result.SpaceAmplification)
	fmt.Printf("Disk Usage: %.1f MB\n", result.TotalDiskMB)

	// Verify reasonable performance
	if result.OpsPerSec < 10000 {
		t.Errorf("Expected at least 10000 ops/sec, got %.0f", result.OpsPerSec)
	}
}
