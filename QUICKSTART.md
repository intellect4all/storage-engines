# Quick Start Guide

Get up and running with HashIndex benchmarks in 2 minutes!

## 1. Build Everything

```bash
# Build benchmark tool
go build -o benchmark ./cmd/benchmark
```

## 2. Run Quick Benchmarks

```bash
./benchmark -quick
```

**Expected Output:**
```
Storage Engine Benchmark Suite
================================
Engine: HashIndex (high-performance)
Duration: 1m0s
Concurrency: 8


=== Running: quick-write-heavy ===
Preloading 1000 keys...
Preload complete
Warming up...
Running benchmark for 10s...

--- Results ---
Throughput: 227531 ops/sec  ← FAST!
Write P99: 164.5µs          ← LOW LATENCY!
Space Amp: 1.17x            ← EFFICIENT!
Zero errors ✅

=== Running: quick-balanced ===
Throughput: 320887 ops/sec  ← EVEN FASTER!
Write P99: 805µs
Read P99: 7µs
Space Amp: 1.17x
```

## 3. Advanced Usage

### Run Specific Workload

```bash
# Just the write-heavy workload
./benchmark -workload quick-write-heavy

# With custom duration
./benchmark -workload quick-balanced -duration 30s
```

### Tune Concurrency

```bash
# Low concurrency (single writer)
./benchmark -quick -concurrency 1

# High concurrency (stress test)
./benchmark -quick -concurrency 32
```

### Full Benchmarks (60s each)

```bash
# Standard workloads (takes ~4 minutes)
./benchmark

# Custom duration
./benchmark -duration 120s -concurrency 16
```

## 4. Understanding the Output

### Throughput
```
Throughput: 320887 ops/sec
```
- Higher is better
- Typical: 200K-550K ops/sec

### Latency Percentiles
```
Write Latency:
  P50:  4.875µs   ← 50% of writes complete in this time
  P95:  8.417µs   ← 95% of writes complete in this time
  P99:  805µs     ← 99% of writes (tail latency)
  P999: 1.02ms    ← 99.9% of writes
```

- **P50** (median) - typical case
- **P99** - what most users experience
- **P999** - worst case for 1 in 1000

### Amplification Factors
```
Amplification:
  Write: 1.17x  ← Disk writes / logical writes
  Space: 1.17x  ← Disk space / logical data size
```

- **1.0x** is perfect (no overhead)
- **1.17x** is excellent (minimal overhead)

### What Good Looks Like
- ✅ Throughput: 200K+ ops/sec
- ✅ Write P99: < 1ms
- ✅ Read P99: < 100µs
- ✅ Space Amp: 1.0-1.5x
- ✅ No errors

## 5. Common Issues

### "Benchmark failed" errors
```bash
# Check disk space
df -h

# Try with shorter duration
./benchmark -quick -duration 5s
```

### Benchmark runs forever
```bash
# Use Ctrl+C to stop
# Or set shorter duration
./benchmark -quick -duration 5s
```

## 6. What to Try Next

### A. Test Different Concurrency Levels
```bash
# Start low
./benchmark -quick -concurrency 1

# Ramp up (watch throughput scale!)
./benchmark -quick -concurrency 4
./benchmark -quick -concurrency 8
./benchmark -quick -concurrency 16
```

### B. Study the Code
1. Read `hashindex/hashindex.go` - see the lock-free techniques
2. Read `hashindex/shard.go` - see how sharding reduces contention
3. Read `hashindex/segment.go` - see reference counting in action

### C. Run Tests
```bash
# Quick test
go test ./hashindex -run TestBasicOperations

# Concurrency stress test
go test ./hashindex -run TestConcurrency

# Benchmark test
go test ./hashindex -run TestQuickBenchmark
```

## 7. CLI Reference

### Benchmark Tool

```bash
./benchmark [OPTIONS]

Options:
  -quick
        Run quick benchmarks (10s each, fewer keys)
  -workload string
        Workload to run: all, quick-write-heavy, quick-balanced, etc (default "all")
  -duration duration
        Duration for each benchmark (default 1m0s)
  -concurrency int
        Number of concurrent workers (default 8)

Examples:
  ./benchmark -quick
  ./benchmark -workload quick-balanced
  ./benchmark -duration 30s -concurrency 16
```

## 8. Key Takeaways

1. **HashIndex is fast** - 300K+ ops/sec in balanced workload
2. **HashIndex is efficient** - 1.17x space amplification (near optimal)
3. **HashIndex is correct** - No race conditions, zero errors
4. **HashIndex is production-ready** - Proper synchronization and compaction

## 9. Next Steps

- Read [README.md](README.md) for full project overview
- Try implementing your own optimizations!

---

**Ready to benchmark? Start with:**
```bash
./benchmark -quick
```
