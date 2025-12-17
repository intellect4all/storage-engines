package lsm

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

// BloomFilter is a probabilistic data structure for membership testing
type BloomFilter struct {
	bits      []byte // Bit array
	numBits   uint64 // Total number of bits
	numHashes uint32 // Number of hash functions
}

// NewBloomFilter creates a new bloom filter with optimal parameters
// expectedKeys: estimated number of keys to insert
// falsePositiveRate: desired false positive rate (e.g., 0.01 for 1%)
func NewBloomFilter(expectedKeys int, falsePositiveRate float64) *BloomFilter {
	// Calculate optimal number of bits
	// m = -(n * ln(p)) / (ln(2)^2)
	numBits := uint64(math.Ceil(-float64(expectedKeys) * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2)))

	// Calculate optimal number of hash functions
	// k = (m/n) * ln(2)
	numHashes := uint32(math.Ceil(float64(numBits) / float64(expectedKeys) * math.Ln2))

	// Ensure at least 1 hash function
	if numHashes == 0 {
		numHashes = 1
	}

	// Allocate bit array (round up to nearest byte)
	numBytes := (numBits + 7) / 8

	return &BloomFilter{
		bits:      make([]byte, numBytes),
		numBits:   numBits,
		numHashes: numHashes,
	}
}

// hash1 and hash2 are used for double hashing
func (bf *BloomFilter) hash1(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

func (bf *BloomFilter) hash2(key string) uint64 {
	h := fnv.New64()
	h.Write([]byte(key))
	return h.Sum64()
}

// getHashes returns k hash values using double hashing
// h_i(x) = (h1(x) + i * h2(x)) mod m
func (bf *BloomFilter) getHashes(key string) []uint64 {
	h1 := bf.hash1(key)
	h2 := bf.hash2(key)

	hashes := make([]uint64, bf.numHashes)
	for i := uint32(0); i < bf.numHashes; i++ {
		hashes[i] = (h1 + uint64(i)*h2) % bf.numBits
	}
	return hashes
}

// Add inserts a key into the bloom filter
func (bf *BloomFilter) Add(key string) {
	hashes := bf.getHashes(key)
	for _, h := range hashes {
		byteIdx := h / 8
		bitIdx := h % 8
		bf.bits[byteIdx] |= 1 << bitIdx
	}
}

// MayContain checks if a key might be in the set
// Returns true if the key might be present (or false positive)
// Returns false if the key is definitely not present
func (bf *BloomFilter) MayContain(key string) bool {
	hashes := bf.getHashes(key)
	for _, h := range hashes {
		byteIdx := h / 8
		bitIdx := h % 8
		if (bf.bits[byteIdx] & (1 << bitIdx)) == 0 {
			return false
		}
	}
	return true
}

// Encode serializes the bloom filter to bytes
// Format: [numBits(8)][numHashes(4)][bits...]
func (bf *BloomFilter) Encode() []byte {
	buf := make([]byte, 12+len(bf.bits))
	binary.LittleEndian.PutUint64(buf[0:], bf.numBits)
	binary.LittleEndian.PutUint32(buf[8:], bf.numHashes)
	copy(buf[12:], bf.bits)
	return buf
}

// DecodeBloomFilter deserializes a bloom filter from bytes
func DecodeBloomFilter(data []byte) *BloomFilter {
	if len(data) < 12 {
		return nil
	}

	numBits := binary.LittleEndian.Uint64(data[0:])
	numHashes := binary.LittleEndian.Uint32(data[8:])
	bits := make([]byte, len(data)-12)
	copy(bits, data[12:])

	return &BloomFilter{
		bits:      bits,
		numBits:   numBits,
		numHashes: numHashes,
	}
}
