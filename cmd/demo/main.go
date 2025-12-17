package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/intellect4all/storage-engines/hashindex"
	"github.com/intellect4all/storage-engines/lsm"
)

func main() {
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("Storage Engines Demo: Hash Index vs LSM-Tree")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()
	fmt.Println("This demo showcases the key differences between two storage engines:")
	fmt.Println("  • Hash Index: Fast point lookups, simple design")
	fmt.Println("  • LSM-Tree:   Range queries, sorted iteration, space efficient")
	fmt.Println()

	// Demo both engines
	demoHashIndex()
	fmt.Println()
	demoLSM()

	// Summary
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("SUMMARY: When to Use Each Engine")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()
	fmt.Println("Use Hash Index when:")
	fmt.Println("  ✓ You only need point lookups (get/put/delete by key)")
	fmt.Println("  ✓ All keys fit in memory")
	fmt.Println("  ✓ Maximum read/write speed is critical")
	fmt.Println("  ✓ Simplicity matters")
	fmt.Println()
	fmt.Println("Use LSM-Tree when:")
	fmt.Println("  ✓ You need range queries or sorted iteration")
	fmt.Println("  ✓ Dataset doesn't fit in memory (billions of keys)")
	fmt.Println("  ✓ Space efficiency is important (1.5x vs 5-6x amplification)")
	fmt.Println("  ✓ You need time-series or analytics workloads")
	fmt.Println()
}

func demoHashIndex() {
	fmt.Println("\n### Hash Index Demo ###")
	fmt.Println(strings.Repeat("-", 40))

	// Create hash index
	config := hashindex.DefaultConfig("./data-hashindex")
	defer os.RemoveAll("./data-hashindex")

	h, err := hashindex.New(config)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	fmt.Println("✓ Created Hash Index storage engine")

	// Put some data
	fmt.Println("\n[Writing data]")
	testData := map[string]string{
		"user:1001":   `{"name": "Alice", "age": 30, "city": "NYC"}`,
		"user:1002":   `{"name": "Bob", "age": 25, "city": "SF"}`,
		"user:1003":   `{"name": "Charlie", "age": 35, "city": "LA"}`,
		"product:101": `{"name": "Laptop", "price": 999.99}`,
		"product:102": `{"name": "Mouse", "price": 29.99}`,
	}

	for key, value := range testData {
		err = h.Put([]byte(key), []byte(value))
		if err != nil {
			log.Printf("Error writing %s: %v", key, err)
		} else {
			fmt.Printf("  PUT %s\n", key)
		}
	}

	// Read data
	fmt.Println("\n[Reading data]")
	for key := range testData {
		value, err := h.Get([]byte(key))
		if err != nil {
			log.Printf("Error reading %s: %v", key, err)
		} else {
			fmt.Printf("  GET %s -> %s\n", key, truncate(string(value), 40))
		}
	}

	// Update data
	fmt.Println("\n[Updating data]")
	h.Put([]byte("user:1001"), []byte(`{"name": "Alice Updated", "age": 31, "city": "NYC"}`))
	fmt.Println("  PUT user:1001 (updated)")

	// Read updated value
	name, _ := h.Get([]byte("user:1001"))
	fmt.Printf("  GET user:1001 -> %s\n", truncate(string(name), 50))

	// Delete data
	fmt.Println("\n[Deleting data]")
	h.Delete([]byte("product:102"))
	fmt.Println("  DELETE product:102")

	// Try to read deleted key
	_, err = h.Get([]byte("product:102"))
	if err != nil {
		fmt.Printf("  GET product:102 -> Key not found (as expected)\n")
	}

	// Show stats
	fmt.Println("\n[Statistics]")
	stats := h.Stats()
	fmt.Printf("  Keys: %d\n", stats.NumKeys)
	fmt.Printf("  Segments: %d\n", stats.NumSegments)
	fmt.Printf("  Disk Usage: %.2f MB\n", float64(stats.TotalDiskSize)/(1024*1024))
	fmt.Printf("  Write Amplification: %.2fx\n", stats.WriteAmp)
	fmt.Printf("  Space Amplification: %.2fx\n", stats.SpaceAmp)
}

func demoLSM() {
	fmt.Println("\n### LSM-Tree Demo ###")
	fmt.Println(strings.Repeat("-", 40))

	// Create LSM-Tree
	config := lsm.DefaultConfig("./data-lsm")
	defer os.RemoveAll("./data-lsm")

	db, err := lsm.New(config)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Println("✓ Created LSM-Tree storage engine")

	// Put some data
	fmt.Println("\n[Writing data]")
	testData := map[string]string{
		"user:1001":   `{"name": "Alice", "age": 30, "city": "NYC"}`,
		"user:1002":   `{"name": "Bob", "age": 25, "city": "SF"}`,
		"user:1003":   `{"name": "Charlie", "age": 35, "city": "LA"}`,
		"product:101": `{"name": "Laptop", "price": 999.99}`,
		"product:102": `{"name": "Mouse", "price": 29.99}`,
	}

	for key, value := range testData {
		err = db.Put(key, []byte(value))
		if err != nil {
			log.Printf("Error writing %s: %v", key, err)
		} else {
			fmt.Printf("  PUT %s\n", key)
		}
	}

	// Read data
	fmt.Println("\n[Reading data]")
	for key := range testData {
		value, found, err := db.Get(key)
		if err != nil {
			log.Printf("Error reading %s: %v", key, err)
		} else if !found {
			log.Printf("Key not found: %s", key)
		} else {
			fmt.Printf("  GET %s -> %s\n", key, truncate(string(value), 40))
		}
	}

	// Update data
	fmt.Println("\n[Updating data]")
	db.Put("user:1001", []byte(`{"name": "Alice Updated", "age": 31, "city": "NYC"}`))
	fmt.Println("  PUT user:1001 (updated)")

	// Read updated value
	name, found, _ := db.Get("user:1001")
	if found {
		fmt.Printf("  GET user:1001 -> %s\n", truncate(string(name), 50))
	}

	// Delete data
	fmt.Println("\n[Deleting data]")
	db.Delete("product:102")
	fmt.Println("  DELETE product:102")

	// Try to read deleted key
	_, found, _ = db.Get("product:102")
	if !found {
		fmt.Printf("  GET product:102 -> Key not found (as expected)\n")
	}

	// Range scan (LSM-Tree's special feature!)
	fmt.Println("\n[Range Scan Capabilities - LSM's Unique Advantage]")
	fmt.Println("Hash Index cannot do this! LSM-Tree maintains sorted order.")

	// Scan 1: Prefix scan (all users)
	fmt.Println("\n1. Prefix scan (user:*):")
	iter := db.Scan("user:", "user:~")
	count := 0
	for iter.Valid() {
		if count < 3 {
			fmt.Printf("   %s -> %s\n", iter.Key(), truncate(string(iter.Value()), 40))
		}
		iter.Next()
		count++
	}
	fmt.Printf("   ... found %d total user keys\n", count)

	// Scan 2: Specific range
	fmt.Println("\n2. Range scan (user:1001 to user:1002):")
	iter2 := db.Scan("user:1001", "user:1003")
	for iter2.Valid() {
		fmt.Printf("   %s -> %s\n", iter2.Key(), truncate(string(iter2.Value()), 40))
		iter2.Next()
	}

	// Scan 3: Different prefix
	fmt.Println("\n3. Scan all products:")
	iter3 := db.Scan("product:", "product:~")
	productCount := 0
	for iter3.Valid() {
		fmt.Printf("   %s -> %s\n", iter3.Key(), truncate(string(iter3.Value()), 40))
		iter3.Next()
		productCount++
	}
	fmt.Printf("   Found %d product keys\n", productCount)

	// Demonstrate sorted iteration
	fmt.Println("\n4. Full database scan (all keys in sorted order):")
	iter4 := db.Scan("", "~")
	allKeys := 0
	lastKey := ""
	for iter4.Valid() {
		currentKey := iter4.Key()
		if allKeys < 5 || allKeys == 5 {
			fmt.Printf("   %s\n", currentKey)
			if allKeys == 5 {
				fmt.Println("   ...")
			}
		}
		lastKey = currentKey
		iter4.Next()
		allKeys++
	}
	if allKeys > 5 {
		fmt.Printf("   %s (last key)\n", lastKey)
	}
	fmt.Printf("   Total: %d keys in sorted order\n", allKeys)

	// Show basic info
	fmt.Println("\n[LSM-Tree Info]")
	fmt.Printf("  L0 files: %d\n", db.GetLevels().NumFiles(0))
	fmt.Printf("  L1 files: %d\n", db.GetLevels().NumFiles(1))
	fmt.Printf("  L2 files: %d\n", db.GetLevels().NumFiles(2))
	fmt.Printf("  Total SSTables: %d\n", db.GetLevels().GetTotalFiles())
	fmt.Printf("  Total Size: %.2f MB\n", float64(db.GetLevels().GetTotalSize())/(1024*1024))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
