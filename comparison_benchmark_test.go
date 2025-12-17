package main

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/intellect4all/storage-engines/hashindex"
	"github.com/intellect4all/storage-engines/lsm"
)

// Benchmark configurations
const (
	smallDataset  = 1000
	mediumDataset = 10000
	largeDataset  = 100000
)

// BenchmarkWritePerformance compares write performance across all storage engines
func BenchmarkWritePerformance(b *testing.B) {
	datasets := []struct {
		name string
		size int
	}{
		{"Small_1K", smallDataset},
		{"Medium_10K", mediumDataset},
		{"Large_100K", largeDataset},
	}

	for _, ds := range datasets {
		b.Run(fmt.Sprintf("LSM_%s", ds.name), func(b *testing.B) {
			benchmarkLSMWrites(b, ds.size)
		})

		b.Run(fmt.Sprintf("HashIndex_%s", ds.name), func(b *testing.B) {
			benchmarkHashIndexWrites(b, ds.size)
		})
	}
}

// BenchmarkReadPerformance compares read performance with pre-populated data
func BenchmarkReadPerformance(b *testing.B) {
	datasets := []struct {
		name string
		size int
	}{
		{"Small_1K", smallDataset},
		{"Medium_10K", mediumDataset},
	}

	for _, ds := range datasets {
		b.Run(fmt.Sprintf("LSM_%s", ds.name), func(b *testing.B) {
			benchmarkLSMReads(b, ds.size)
		})

		b.Run(fmt.Sprintf("HashIndex_%s", ds.name), func(b *testing.B) {
			benchmarkHashIndexReads(b, ds.size)
		})
	}
}

// BenchmarkMixedWorkload tests realistic workloads
func BenchmarkMixedWorkload(b *testing.B) {
	workloads := []struct {
		name       string
		readRatio  float64
		writeRatio float64
	}{
		{"Read_Heavy_90_10", 0.9, 0.1},
		{"Balanced_50_50", 0.5, 0.5},
		{"Write_Heavy_10_90", 0.1, 0.9},
	}

	for _, wl := range workloads {
		b.Run(fmt.Sprintf("LSM_%s", wl.name), func(b *testing.B) {
			benchmarkLSMMixed(b, mediumDataset, wl.readRatio)
		})

		b.Run(fmt.Sprintf("HashIndex_%s", wl.name), func(b *testing.B) {
			benchmarkHashIndexMixed(b, mediumDataset, wl.readRatio)
		})
	}
}

// LSM Benchmark Implementations
func benchmarkLSMWrites(b *testing.B, numOps int) {
	dir := fmt.Sprintf("/tmp/bench-lsm-write-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := lsm.DefaultConfig(dir)
	db, err := lsm.New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < numOps; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		if err := db.Put(key, value); err != nil {
			b.Fatal(err)
		}
	}

	elapsed := time.Since(start)
	b.StopTimer()

	opsPerSec := float64(numOps) / elapsed.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
	b.ReportMetric(float64(elapsed.Milliseconds()), "total_ms")
}

func benchmarkLSMReads(b *testing.B, numKeys int) {
	dir := fmt.Sprintf("/tmp/bench-lsm-read-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := lsm.DefaultConfig(dir)
	db, err := lsm.New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Populate
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		db.Put(key, value)
	}
	time.Sleep(200 * time.Millisecond) // Allow compaction

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		keyIdx := rand.Intn(numKeys)
		key := fmt.Sprintf("key%010d", keyIdx)
		_, found, err := db.Get(key)
		if err != nil {
			b.Fatal(err)
		}
		if !found {
			b.Fatalf("Key not found: %s", key)
		}
	}

	elapsed := time.Since(start)
	b.StopTimer()

	opsPerSec := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func benchmarkLSMMixed(b *testing.B, numKeys int, readRatio float64) {
	dir := fmt.Sprintf("/tmp/bench-lsm-mixed-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := lsm.DefaultConfig(dir)
	db, err := lsm.New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Populate
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		db.Put(key, value)
	}
	time.Sleep(200 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rand.Float64() < readRatio {
			keyIdx := rand.Intn(numKeys)
			key := fmt.Sprintf("key%010d", keyIdx)
			db.Get(key)
		} else {
			keyIdx := rand.Intn(numKeys * 2)
			key := fmt.Sprintf("key%010d", keyIdx)
			value := []byte(fmt.Sprintf("value%010d", keyIdx))
			db.Put(key, value)
		}
	}
}

// HashIndex Benchmark Implementations
func benchmarkHashIndexWrites(b *testing.B, numOps int) {
	dir := fmt.Sprintf("/tmp/bench-hash-write-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	db, err := hashindex.New(hashindex.Config{DataDir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < numOps; i++ {
		key := []byte(fmt.Sprintf("key%010d", i))
		value := []byte(fmt.Sprintf("value%010d", i))
		if err := db.Put(key, value); err != nil {
			b.Fatal(err)
		}
	}

	elapsed := time.Since(start)
	b.StopTimer()

	opsPerSec := float64(numOps) / elapsed.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
	b.ReportMetric(float64(elapsed.Milliseconds()), "total_ms")
}

func benchmarkHashIndexReads(b *testing.B, numKeys int) {
	dir := fmt.Sprintf("/tmp/bench-hash-read-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	db, err := hashindex.New(hashindex.Config{DataDir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Populate
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%010d", i))
		value := []byte(fmt.Sprintf("value%010d", i))
		db.Put(key, value)
	}

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		keyIdx := rand.Intn(numKeys)
		key := []byte(fmt.Sprintf("key%010d", keyIdx))
		_, err := db.Get(key)
		if err != nil {
			b.Fatal(err)
		}
	}

	elapsed := time.Since(start)
	b.StopTimer()

	opsPerSec := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

func benchmarkHashIndexMixed(b *testing.B, numKeys int, readRatio float64) {
	dir := fmt.Sprintf("/tmp/bench-hash-mixed-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	db, err := hashindex.New(hashindex.Config{DataDir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Populate
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%010d", i))
		value := []byte(fmt.Sprintf("value%010d", i))
		db.Put(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rand.Float64() < readRatio {
			keyIdx := rand.Intn(numKeys)
			key := []byte(fmt.Sprintf("key%010d", keyIdx))
			db.Get(key)
		} else {
			keyIdx := rand.Intn(numKeys * 2)
			key := []byte(fmt.Sprintf("key%010d", keyIdx))
			value := []byte(fmt.Sprintf("value%010d", keyIdx))
			db.Put(key, value)
		}
	}
}

// BenchmarkRangeScanCapability demonstrates LSM's unique advantage
func BenchmarkRangeScanCapability(b *testing.B) {
	dir := fmt.Sprintf("/tmp/bench-lsm-range-%d", time.Now().UnixNano())
	defer os.RemoveAll(dir)

	config := lsm.DefaultConfig(dir)
	db, err := lsm.New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Populate with sorted data
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key%010d", i)
		value := []byte(fmt.Sprintf("value%010d", i))
		db.Put(key, value)
	}
	time.Sleep(200 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := db.Scan("", "")
		count := 0
		for iter.Valid() {
			count++
			iter.Next()
		}
	}
}

// BenchmarkNegativeLookups tests bloom filter effectiveness
func BenchmarkNegativeLookups(b *testing.B) {
	b.Run("LSM_WithBloomFilter", func(b *testing.B) {
		dir := fmt.Sprintf("/tmp/bench-lsm-neg-%d", time.Now().UnixNano())
		defer os.RemoveAll(dir)

		config := lsm.DefaultConfig(dir)
		db, err := lsm.New(config)
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		// Populate
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("key%010d", i)
			value := []byte("value")
			db.Put(key, value)
		}
		time.Sleep(200 * time.Millisecond)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%010d", 10000+i) // Non-existent keys
			db.Get(key)
		}
	})

	b.Run("HashIndex_NoBloomFilter", func(b *testing.B) {
		dir := fmt.Sprintf("/tmp/bench-hash-neg-%d", time.Now().UnixNano())
		defer os.RemoveAll(dir)

		db, err := hashindex.New(hashindex.Config{DataDir: dir})
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		// Populate
		for i := 0; i < 10000; i++ {
			key := []byte(fmt.Sprintf("key%010d", i))
			value := []byte("value")
			db.Put(key, value)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key%010d", 10000+i)) // Non-existent keys
			db.Get(key)
		}
	})
}
