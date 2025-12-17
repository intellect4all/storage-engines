package benchmark

import (
	"encoding/binary"
	"fmt"
	"math"
	mrand "math/rand"
	"sync/atomic"
)

// KeyDistribution defines how keys are accessed
type KeyDistribution string

const (
	DistUniform    KeyDistribution = "uniform"    // All keys equally likely
	DistZipfian    KeyDistribution = "zipfian"    // 80/20 rule (realistic)
	DistSequential KeyDistribution = "sequential" // Sequential access
	DistLatest     KeyDistribution = "latest"     // Recent keys (time-series)
)

// KeyGenerator generates keys according to distribution
type KeyGenerator struct {
	numKeys      int
	keySize      int
	distribution KeyDistribution
	rng          *mrand.Rand

	// For Zipfian distribution
	zipf *mrand.Zipf

	// For sequential
	seqCounter atomic.Int64
}

func NewKeyGenerator(numKeys, keySize int, distribution KeyDistribution, seed int64) *KeyGenerator {
	rng := mrand.New(mrand.NewSource(seed))

	kg := &KeyGenerator{
		numKeys:      numKeys,
		keySize:      keySize,
		distribution: distribution,
		rng:          rng,
	}

	// Setup Zipfian if needed (80/20 distribution)
	if distribution == DistZipfian {
		kg.zipf = mrand.NewZipf(rng, 1.1, 1, uint64(numKeys))
	}

	return kg
}

func (kg *KeyGenerator) NextKey() []byte {
	var keyNum int

	switch kg.distribution {
	case DistUniform:
		keyNum = kg.rng.Intn(kg.numKeys)

	case DistZipfian:
		keyNum = int(kg.zipf.Uint64())

	case DistSequential:
		keyNum = int(kg.seqCounter.Add(1) % int64(kg.numKeys))

	case DistLatest:
		// Access recent keys more often (exponential decay)
		range_ := kg.numKeys / 10
		if range_ < 100 {
			range_ = 100
		}
		offset := int(math.Abs(kg.rng.NormFloat64()) * float64(range_))
		keyNum = kg.numKeys - 1 - offset
		if keyNum < 0 {
			keyNum = 0
		}

	default:
		keyNum = kg.rng.Intn(kg.numKeys)
	}

	return kg.formatKey(keyNum)
}

func (kg *KeyGenerator) GenerateSequential(n int) []byte {
	return kg.formatKey(n)
}

func (kg *KeyGenerator) formatKey(n int) []byte {
	// Format: user<padded-number>
	// Example: user0000012345
	key := fmt.Sprintf("user%010d", n)

	if len(key) < kg.keySize {
		padding := make([]byte, kg.keySize-len(key))
		// Fill padding with deterministic data based on key number
		if len(padding) >= 8 {
			binary.LittleEndian.PutUint64(padding, uint64(n))
		} else {
			// For small padding, just use sequential bytes
			for i := range padding {
				padding[i] = byte(n + i)
			}
		}
		return append([]byte(key), padding...)
	}

	return []byte(key)[:kg.keySize]
}
