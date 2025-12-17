package benchmark

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/intellect4all/storage-engines/common"
)

// ComparisonSuite runs benchmarks across multiple engines
type ComparisonSuite struct {
	configs []Config
}

func NewComparisonSuite() *ComparisonSuite {
	return &ComparisonSuite{
		configs: StandardWorkloads(),
	}
}

// SetWorkloads sets custom workload configurations
func (cs *ComparisonSuite) SetWorkloads(configs []Config) {
	cs.configs = configs
}

// StandardWorkloads returns common benchmark scenarios
func StandardWorkloads() []Config {
	return []Config{
		{
			Name:            "write-heavy-uniform",
			WorkloadType:    WorkloadWriteHeavy,
			KeyDistribution: DistUniform,
			NumKeys:         1000000,
			KeySize:         16,
			ValueSize:       100,
			Duration:        60 * time.Second,
			Concurrency:     8,
			PreloadKeys:     100000,
			Seed:            12345,
		},
		{
			Name:            "read-heavy-zipfian",
			WorkloadType:    WorkloadReadHeavy,
			KeyDistribution: DistZipfian,
			NumKeys:         1000000,
			KeySize:         16,
			ValueSize:       100,
			Duration:        60 * time.Second,
			Concurrency:     8,
			PreloadKeys:     500000,
			Seed:            12345,
		},
		{
			Name:            "balanced-uniform",
			WorkloadType:    WorkloadBalanced,
			KeyDistribution: DistUniform,
			NumKeys:         1000000,
			KeySize:         16,
			ValueSize:       100,
			Duration:        60 * time.Second,
			Concurrency:     8,
			PreloadKeys:     100000,
			Seed:            12345,
		},
		{
			Name:            "write-only-sequential",
			WorkloadType:    WorkloadWriteOnly,
			KeyDistribution: DistSequential,
			NumKeys:         1000000,
			KeySize:         16,
			ValueSize:       1000, // Larger values
			Duration:        30 * time.Second,
			Concurrency:     1,
			PreloadKeys:     0,
			Seed:            12345,
		},
	}
}

// QuickWorkloads returns faster workloads for testing
// Designed to work with both Hash Index and LSM-Tree defaults
// LSM default memtable = 4MB, so we need > 4MB of data to trigger flushes
func QuickWorkloads() []Config {
	return []Config{
		{
			Name:            "quick-write-heavy",
			WorkloadType:    WorkloadWriteHeavy,
			KeyDistribution: DistUniform,
			NumKeys:         50000, // 50k keys × 132 bytes = 6.3 MB (triggers LSM flushes)
			KeySize:         16,
			ValueSize:       100,
			Duration:        15 * time.Second,
			Concurrency:     8,
			PreloadKeys:     5000, // Start with some data
			Seed:            12345,
		},
		{
			Name:            "quick-balanced",
			WorkloadType:    WorkloadBalanced,
			KeyDistribution: DistUniform,
			NumKeys:         50000, // 50k keys × 132 bytes = 6.3 MB
			KeySize:         16,
			ValueSize:       100,
			Duration:        15 * time.Second,
			Concurrency:     8,
			PreloadKeys:     10000, // More preload for read-heavy portion
			Seed:            12345,
		},
		{
			Name:            "quick-read-heavy",
			WorkloadType:    WorkloadReadHeavy,
			KeyDistribution: DistZipfian, // Realistic: some keys accessed more
			NumKeys:         50000,
			KeySize:         16,
			ValueSize:       100,
			Duration:        15 * time.Second,
			Concurrency:     8,
			PreloadKeys:     30000, // Need data to read
			Seed:            12345,
		},
	}
}

// RunComparison runs all workloads against multiple engines
func (cs *ComparisonSuite) RunComparison(engines map[string]common.StorageEngine) map[string][]*Result {
	results := make(map[string][]*Result)

	for engineName, engine := range engines {
		fmt.Printf("\n=== Benchmarking %s ===\n", engineName)
		engineResults := make([]*Result, 0)

		for _, config := range cs.configs {
			fmt.Printf("\nRunning: %s\n", config.Name)

			bench := NewBenchmark(engine, config)
			result, err := bench.Run()
			if err != nil {
				fmt.Printf("ERROR: %v\n", err)
				continue
			}

			engineResults = append(engineResults, result)
			cs.printResult(result)
		}

		results[engineName] = engineResults
	}

	return results
}

func (cs *ComparisonSuite) printResult(r *Result) {
	fmt.Printf("\nResults for: %s\n", r.Config.Name)
	fmt.Printf("  Throughput: %.0f ops/sec\n", r.OpsPerSec)
	fmt.Printf("  Total Ops: %d (writes: %d, reads: %d)\n",
		r.TotalOps, r.WriteOps, r.ReadOps)

	if r.WriteOps > 0 {
		fmt.Printf("  Write Latency (μs):\n")
		fmt.Printf("    p50:  %6d\n", r.WriteLatency.P50.Microseconds())
		fmt.Printf("    p95:  %6d\n", r.WriteLatency.P95.Microseconds())
		fmt.Printf("    p99:  %6d\n", r.WriteLatency.P99.Microseconds())
		fmt.Printf("    p999: %6d\n", r.WriteLatency.P999.Microseconds())
	}

	if r.ReadOps > 0 {
		fmt.Printf("  Read Latency (μs):\n")
		fmt.Printf("    p50:  %6d\n", r.ReadLatency.P50.Microseconds())
		fmt.Printf("    p95:  %6d\n", r.ReadLatency.P95.Microseconds())
		fmt.Printf("    p99:  %6d\n", r.ReadLatency.P99.Microseconds())
		fmt.Printf("    p999: %6d\n", r.ReadLatency.P999.Microseconds())
	}

	fmt.Printf("  Amplification:\n")
	fmt.Printf("    Write: %.2fx\n", r.WriteAmplification)
	fmt.Printf("    Space: %.2fx\n", r.SpaceAmplification)
	fmt.Printf("  Disk Usage: %.1f MB\n", r.TotalDiskMB)
}

// PrintComparisonTable prints a comparison table
func (cs *ComparisonSuite) PrintComparisonTable(results map[string][]*Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "\n=== THROUGHPUT COMPARISON (ops/sec) ===")
	fmt.Fprintf(w, "Workload\t")
	for engine := range results {
		fmt.Fprintf(w, "%s\t", engine)
	}
	fmt.Fprintln(w)

	// Assuming all engines ran same workloads
	for i, config := range cs.configs {
		fmt.Fprintf(w, "%s\t", config.Name)
		for engine := range results {
			if i < len(results[engine]) {
				fmt.Fprintf(w, "%.0f\t", results[engine][i].OpsPerSec)
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	// Latency comparison
	fmt.Fprintln(w, "\n=== WRITE P99 LATENCY COMPARISON (μs) ===")
	fmt.Fprintf(w, "Workload\t")
	for engine := range results {
		fmt.Fprintf(w, "%s\t", engine)
	}
	fmt.Fprintln(w)

	for i, config := range cs.configs {
		fmt.Fprintf(w, "%s\t", config.Name)
		for engine := range results {
			if i < len(results[engine]) && results[engine][i].WriteOps > 0 {
				fmt.Fprintf(w, "%d\t", results[engine][i].WriteLatency.P99.Microseconds())
			} else {
				fmt.Fprintf(w, "N/A\t")
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	// Amplification comparison
	fmt.Fprintln(w, "\n=== WRITE AMPLIFICATION COMPARISON ===")
	fmt.Fprintf(w, "Workload\t")
	for engine := range results {
		fmt.Fprintf(w, "%s\t", engine)
	}
	fmt.Fprintln(w)

	for i, config := range cs.configs {
		fmt.Fprintf(w, "%s\t", config.Name)
		for engine := range results {
			if i < len(results[engine]) {
				fmt.Fprintf(w, "%.2fx\t", results[engine][i].WriteAmplification)
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}
