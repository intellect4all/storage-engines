package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/intellect4all/storage-engines/common"
	"github.com/intellect4all/storage-engines/common/benchmark"
	"github.com/intellect4all/storage-engines/hashindex"
	"github.com/intellect4all/storage-engines/lsm"
)

func main() {
	quick := flag.Bool("quick", false, "Run quick benchmarks (shorter duration)")
	workload := flag.String("workload", "all", "Workload to run (all, write-heavy, read-heavy, balanced, write-only)")
	duration := flag.Duration("duration", 60*time.Second, "Duration for each benchmark")
	concurrency := flag.Int("concurrency", 8, "Number of concurrent workers")
	engine := flag.String("engine", "compare", "Engine to benchmark: hashindex, lsm, or compare (default: compare)")
	flag.Parse()

	fmt.Println("Storage Engine Benchmark Suite")
	fmt.Println("================================")
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Mode: %s\n\n", *engine)

	var configs []benchmark.Config
	if *quick {
		configs = benchmark.QuickWorkloads()
	} else {
		configs = benchmark.StandardWorkloads()
	}

	// Apply custom duration and concurrency if specified
	if flag.Lookup("duration").Value.String() != flag.Lookup("duration").DefValue {
		for i := range configs {
			configs[i].Duration = *duration
		}
	}

	if flag.Lookup("concurrency").Value.String() != flag.Lookup("concurrency").DefValue {
		for i := range configs {
			configs[i].Concurrency = *concurrency
		}
	}

	// Filter workloads if specified
	if *workload != "all" {
		filtered := make([]benchmark.Config, 0)
		for _, config := range configs {
			if config.Name == *workload {
				filtered = append(filtered, config)
			}
		}
		if len(filtered) == 0 {
			fmt.Printf("Unknown workload: %s\n", *workload)
			os.Exit(1)
		}
		configs = filtered
	}

	switch *engine {
	case "hashindex":
		runHashIndex(configs)
	case "lsm":
		runLSM(configs)
	case "compare":
		runComparison(configs)
	default:
		fmt.Printf("Unknown engine: %s (must be hashindex, lsm, or compare)\n", *engine)
		os.Exit(1)
	}
}

func runHashIndex(configs []benchmark.Config) {
	fmt.Println("=== Hash Index Benchmark ===\n")

	dir, err := os.MkdirTemp("", "benchmark-hashindex-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	config := hashindex.DefaultConfig(dir)
	config.SyncOnWrite = false // Async writes for better performance

	h, err := hashindex.New(config)
	if err != nil {
		fmt.Printf("Failed to create HashIndex: %v\n", err)
		os.Exit(1)
	}
	defer h.Close()

	results := runBenchmarks(h, "HashIndex", configs)
	printSummaryTable(results)
}

func runLSM(configs []benchmark.Config) {
	fmt.Println("=== LSM-Tree Benchmark ===\n")

	dir, err := os.MkdirTemp("", "benchmark-lsm-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	lsmConfig := lsm.DefaultConfig(dir)
	l, err := lsm.NewAdapter(lsmConfig)
	if err != nil {
		fmt.Printf("Failed to create LSM: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	results := runBenchmarks(l, "LSM-Tree", configs)
	printSummaryTable(results)

	// Run LSM-specific range scan benchmark
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("LSM-TREE RANGE SCAN BENCHMARK")
	fmt.Println(strings.Repeat("=", 80))
	runRangeScanBenchmark(l)
}

func runComparison(configs []benchmark.Config) {
	fmt.Println("=== Comparing HashIndex vs. LSM-Tree ===\n")

	// Create temp directories
	hashDir, err := os.MkdirTemp("", "benchmark-hashindex-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(hashDir)

	lsmDir, err := os.MkdirTemp("", "benchmark-lsm-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(lsmDir)

	// Create engines
	hashConfig := hashindex.DefaultConfig(hashDir)
	hashConfig.SyncOnWrite = false
	h, err := hashindex.New(hashConfig)
	if err != nil {
		fmt.Printf("Failed to create HashIndex: %v\n", err)
		os.Exit(1)
	}
	defer h.Close()

	lsmConfig := lsm.DefaultConfig(lsmDir)
	l, err := lsm.NewAdapter(lsmConfig)
	if err != nil {
		fmt.Printf("Failed to create LSM: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	// Create engine map
	engines := map[string]common.StorageEngine{
		"HashIndex": h,
		"LSM-Tree":  l,
	}

	// Run comparison
	suite := benchmark.NewComparisonSuite()
	suite.SetWorkloads(configs)
	results := suite.RunComparison(engines)

	// Print comparison table
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("COMPARISON RESULTS")
	fmt.Println(strings.Repeat("=", 80))
	suite.PrintComparisonTable(results)
}

func runBenchmarks(engine common.StorageEngine, name string, configs []benchmark.Config) []*benchmark.Result {
	results := make([]*benchmark.Result, 0)

	for _, config := range configs {
		fmt.Printf("\n=== Running: %s ===\n", config.Name)

		bench := benchmark.NewBenchmark(engine, config)
		result, err := bench.Run()
		if err != nil {
			fmt.Printf("Benchmark failed: %v\n", err)
			continue
		}

		results = append(results, result)
		printResult(result)
	}

	return results
}

func printResult(r *benchmark.Result) {
	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Throughput: %.0f ops/sec\n", r.OpsPerSec)
	fmt.Printf("Total Ops: %d (writes: %d, reads: %d)\n",
		r.TotalOps, r.WriteOps, r.ReadOps)

	if r.WriteOps > 0 {
		fmt.Printf("\nWrite Latency:\n")
		fmt.Printf("  Min:  %8s\n", r.WriteLatency.Min)
		fmt.Printf("  Mean: %8s\n", r.WriteLatency.Mean)
		fmt.Printf("  P50:  %8s\n", r.WriteLatency.P50)
		fmt.Printf("  P95:  %8s\n", r.WriteLatency.P95)
		fmt.Printf("  P99:  %8s\n", r.WriteLatency.P99)
		fmt.Printf("  P999: %8s\n", r.WriteLatency.P999)
		fmt.Printf("  Max:  %8s\n", r.WriteLatency.Max)
	}

	if r.ReadOps > 0 {
		fmt.Printf("\nRead Latency:\n")
		fmt.Printf("  Min:  %8s\n", r.ReadLatency.Min)
		fmt.Printf("  Mean: %8s\n", r.ReadLatency.Mean)
		fmt.Printf("  P50:  %8s\n", r.ReadLatency.P50)
		fmt.Printf("  P95:  %8s\n", r.ReadLatency.P95)
		fmt.Printf("  P99:  %8s\n", r.ReadLatency.P99)
		fmt.Printf("  P999: %8s\n", r.ReadLatency.P999)
		fmt.Printf("  Max:  %8s\n", r.ReadLatency.Max)
	}

	fmt.Printf("\nAmplification:\n")
	fmt.Printf("  Write: %.2fx\n", r.WriteAmplification)
	fmt.Printf("  Space: %.2fx\n", r.SpaceAmplification)
	fmt.Printf("\nDisk Usage: %.1f MB\n", r.TotalDiskMB)
}

func printSummaryTable(results []*benchmark.Result) {
	if len(results) == 0 {
		return
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("BENCHMARK SUMMARY")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Printf("\n%-25s %12s %12s %12s %12s\n",
		"Workload", "Throughput", "Write P99", "Read P99", "Write Amp")
	fmt.Println("--------------------------------------------------------------------------------")

	for _, r := range results {
		writeP99 := "N/A"
		if r.WriteOps > 0 {
			writeP99 = fmt.Sprintf("%s", r.WriteLatency.P99)
		}

		readP99 := "N/A"
		if r.ReadOps > 0 {
			readP99 = fmt.Sprintf("%s", r.ReadLatency.P99)
		}

		fmt.Printf("%-25s %10.0f/s %12s %12s %11.2fx\n",
			r.Config.Name,
			r.OpsPerSec,
			writeP99,
			readP99,
			r.WriteAmplification)
	}
}

func runRangeScanBenchmark(adapter *lsm.Adapter) {
	fmt.Println("\nPreparing range scan test data...")

	// Insert sequential keys for range scanning
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("user:%06d", i)
		value := []byte(fmt.Sprintf(`{"id": %d, "name": "user%d"}`, i, i))
		adapter.Put([]byte(key), value)
	}

	fmt.Println("Running range scans...")

	// Test different range sizes
	ranges := []struct {
		name  string
		start string
		end   string
	}{
		{"Small (100 keys)", "user:000000", "user:000100"},
		{"Medium (1000 keys)", "user:000000", "user:001000"},
		{"Large (5000 keys)", "user:000000", "user:005000"},
		{"Full scan", "user:000000", "user:999999"},
	}

	for _, r := range ranges {
		start := time.Now()
		iter := adapter.Scan(r.start, r.end)
		count := 0
		for iter.Valid() {
			count++
			iter.Next()
		}
		elapsed := time.Since(start)

		throughput := float64(count) / elapsed.Seconds()
		avgLatency := elapsed / time.Duration(count)

		fmt.Printf("\n%s:\n", r.name)
		fmt.Printf("  Keys scanned: %d\n", count)
		fmt.Printf("  Duration:     %v\n", elapsed)
		fmt.Printf("  Throughput:   %.0f keys/sec\n", throughput)
		fmt.Printf("  Avg latency:  %v per key\n", avgLatency)
	}
}
