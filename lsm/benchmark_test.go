package lsm

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"
)

func BenchmarkWriteHeavy(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-write-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		err := lsm.Put(key, value)
		if err != nil {
			b.Fatalf("Put failed: %v", err)
		}
	}
	b.StopTimer()

	// Report throughput
	duration := b.Elapsed()
	opsPerSec := float64(b.N) / duration.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func BenchmarkReadHeavy(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-read-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Populate with data
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		lsm.Put(key, value)
	}

	// Wait for flush/compaction
	time.Sleep(500 * time.Millisecond)

	// Benchmark reads
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keyIdx := rand.Intn(numKeys)
		key := fmt.Sprintf("key%010d", keyIdx)
		_, found, err := lsm.Get(key)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}
		if !found {
			b.Fatalf("Key not found: %s", key)
		}
	}
	b.StopTimer()

	duration := b.Elapsed()
	opsPerSec := float64(b.N) / duration.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func BenchmarkBalanced(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-balanced-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Populate with initial data
	numKeys := 5000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		lsm.Put(key, value)
	}

	time.Sleep(300 * time.Millisecond)

	// 50% reads, 50% writes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rand.Float32() < 0.5 {
			// Read
			keyIdx := rand.Intn(numKeys)
			key := fmt.Sprintf("key%010d", keyIdx)
			lsm.Get(key)
		} else {
			// Write
			keyIdx := rand.Intn(numKeys * 2)
			key := fmt.Sprintf("key%010d", keyIdx)
			value := []byte(fmt.Sprintf("value%010d", keyIdx))
			lsm.Put(key, value)
		}
	}
	b.StopTimer()

	duration := b.Elapsed()
	opsPerSec := float64(b.N) / duration.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func BenchmarkWriteThroughput(b *testing.B) {
	benchmarks := []struct {
		name    string
		numOps  int
	}{
		{"10K", 10000},
		{"50K", 50000},
		{"100K", 100000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			dir := fmt.Sprintf("/tmp/lsm-bench-throughput-%d", time.Now().UnixNano())
			defer os.RemoveAll(dir)

			config := DefaultConfig(dir)
			lsm, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create LSM: %v", err)
			}
			defer lsm.Close()

			b.ResetTimer()
			start := time.Now()

			for i := 0; i < bm.numOps; i++ {
				key := fmt.Sprintf("key%010d", i)
				value := []byte(fmt.Sprintf("value%010d", i))
				lsm.Put(key, value)
			}

			elapsed := time.Since(start)
			b.StopTimer()

			opsPerSec := float64(bm.numOps) / elapsed.Seconds()
			b.ReportMetric(opsPerSec, "ops/sec")
			b.ReportMetric(elapsed.Seconds()*1000, "ms")
		})
	}
}

func BenchmarkReadLatency(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-latency-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Populate
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		lsm.Put(key, value)
	}

	time.Sleep(500 * time.Millisecond)

	// Measure latencies
	latencies := make([]time.Duration, 1000)

	b.ResetTimer()
	for i := 0; i < 1000; i++ {
		keyIdx := rand.Intn(numKeys)
		key := fmt.Sprintf("key%010d", keyIdx)

		start := time.Now()
		lsm.Get(key)
		latencies[i] = time.Since(start)
	}
	b.StopTimer()

	// Calculate percentiles
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 := latencies[500].Microseconds()
	p95 := latencies[950].Microseconds()
	p99 := latencies[990].Microseconds()

	b.ReportMetric(float64(p50), "p50_µs")
	b.ReportMetric(float64(p95), "p95_µs")
	b.ReportMetric(float64(p99), "p99_µs")
}

func BenchmarkNegativeLookup(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-negative-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Populate with keys 0-9999
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		lsm.Put(key, value)
	}

	time.Sleep(500 * time.Millisecond)

	// Query for non-existent keys (10000+)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%010d", numKeys+i)
		_, found, err := lsm.Get(key)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}
		if found {
			b.Fatalf("Non-existent key found!")
		}
	}
	b.StopTimer()

	duration := b.Elapsed()
	opsPerSec := float64(b.N) / duration.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func BenchmarkUpdateExisting(b *testing.B) {
	dir := fmt.Sprintf("/tmp/lsm-bench-update-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := DefaultConfig(dir)
	lsm, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create LSM: %v", err)
	}
	defer lsm.Close()

	// Populate
	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		lsm.Put(key, value)
	}

	// Benchmark updates
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keyIdx := rand.Intn(numKeys)
		key := fmt.Sprintf("key%010d", keyIdx)
		value := []byte(fmt.Sprintf("newvalue%010d", i))
		err := lsm.Put(key, value)
		if err != nil {
			b.Fatalf("Put failed: %v", err)
		}
	}
	b.StopTimer()

	duration := b.Elapsed()
	opsPerSec := float64(b.N) / duration.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}
