package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/intellect4all/storage-engines/common/benchmark"
	"github.com/intellect4all/storage-engines/hashindex"
)

func main() {

	quick := flag.Bool("quick", false, "Run quick benchmarks (shorter duration)")
	workload := flag.String("workload", "all", "Workload to run (all, write-heavy, read-heavy, balanced, write-only)")
	duration := flag.Duration("duration", 60*time.Second, "Duration for each benchmark")
	concurrency := flag.Int("concurrency", 8, "Number of concurrent workers")
	flag.Parse()

	fmt.Println("Storage Engine Benchmark Suite")
	fmt.Println("================================")
	fmt.Printf("Engine: HashIndex (high-performance)\n")
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Printf("Concurrency: %d\n\n", *concurrency)

	dir, err := os.MkdirTemp("", "benchmark-*")
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

	var configs []benchmark.Config
	if *quick {
		configs = benchmark.QuickWorkloads()
	} else {
		configs = benchmark.StandardWorkloads()
	}

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

	results := make([]*benchmark.Result, 0)
	for _, config := range configs {
		fmt.Printf("\n=== Running: %s ===\n", config.Name)

		bench := benchmark.NewBenchmark(h, config)

		result, err := bench.Run()
		if err != nil {
			fmt.Printf("Benchmark failed: %v\n", err)
			continue
		}

		results = append(results, result)
		printResult(result)
	}

	fmt.Println("\n================================")
	fmt.Println("BENCHMARK SUMMARY")
	fmt.Println("================================")
	printSummaryTable(results)
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
