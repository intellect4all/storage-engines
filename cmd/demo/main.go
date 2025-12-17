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
	err = h.Put([]byte("name"), []byte("Alice"))
	if err != nil {

		log.Printf("Error: %v", err)
	}
	err = h.Put([]byte("age"), []byte("30"))
	if err != nil {
		log.Printf("Error: %v", err)
	}
	h.Put([]byte("city"), []byte("NYC"))

	// Get data
	name, _ := h.Get([]byte("name"))
	fmt.Printf("Name: %s\n", name)

	age, _ := h.Get([]byte("age"))
	fmt.Printf("Age: %s\n", age)

	city, _ := h.Get([]byte("city"))
	fmt.Printf("City: %s\n", city)

	// Put some data
	h.Put([]byte("name"), []byte("John"))
	name, _ = h.Get([]byte("name"))
	fmt.Printf("Name: %s\n", name)

	// Show stats
	stats := h.Stats()
	fmt.Printf("Stats: %+v\n", stats)
}
