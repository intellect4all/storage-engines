package main

import (
	"fmt"
	"log"

	"github.com/intellect4all/storage-engines/hashindex"
)

func main() {
	// Create hash index
	config := hashindex.DefaultConfig("./data")
	h, err := hashindex.New(config)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	// Put some data
	h.Put([]byte("name"), []byte("Alice"))
	h.Put([]byte("age"), []byte("30"))
	h.Put([]byte("city"), []byte("NYC"))

	// Get data
	name, _ := h.Get([]byte("name"))
	fmt.Printf("Name: %s\n", name)

	// Show stats
	stats := h.Stats()
	fmt.Printf("Stats: %+v\n", stats)
}
