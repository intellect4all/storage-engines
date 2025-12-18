package btree

import (
	"fmt"
	"testing"
)

func TestVarintEncoding(t *testing.T) {
	tests := []struct {
		value    uint16
		expected int // expected size in bytes
	}{
		{0, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
		{65535, 3},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("value_%d", tt.value), func(t *testing.T) {
			// Test encoding
			buf := make([]byte, 10)
			n := putUvarint16(buf, tt.value)

			if n != tt.expected {
				t.Errorf("putUvarint16(%d) = %d bytes, want %d bytes", tt.value, n, tt.expected)
			}

			// Test decoding
			decoded, n2 := uvarint16(buf)
			if n2 != n {
				t.Errorf("uvarint16 returned %d bytes, want %d bytes", n2, n)
			}

			if decoded != tt.value {
				t.Errorf("uvarint16 = %d, want %d", decoded, tt.value)
			}

			// Test size calculation
			size := varintSize16(tt.value)
			if size != tt.expected {
				t.Errorf("varintSize16(%d) = %d, want %d", tt.value, size, tt.expected)
			}
		})
	}
}

func TestVarintRoundTrip(t *testing.T) {
	// Test all uint16 values
	buf := make([]byte, 10)

	for i := uint16(0); i < 1000; i++ {
		n := putUvarint16(buf, i)
		decoded, n2 := uvarint16(buf)

		if n != n2 {
			t.Errorf("Round trip size mismatch for %d: encoded %d bytes, decoded %d bytes", i, n, n2)
		}

		if decoded != i {
			t.Errorf("Round trip value mismatch: encoded %d, decoded %d", i, decoded)
		}
	}
}

func TestVarintSpaceSavings(t *testing.T) {
	// Calculate space savings for typical key sizes
	testCases := []struct {
		keySize   uint16
		valueSize uint16
	}{
		{10, 20},   // Small key/value
		{50, 100},  // Medium key/value
		{100, 200}, // Large key/value
		{127, 127}, // Edge case (1 byte varint)
		{128, 128}, // Edge case (2 bytes varint)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("key_%d_value_%d", tc.keySize, tc.valueSize), func(t *testing.T) {
			// V1 (fixed): 2 bytes for key size + 2 bytes for value size = 4 bytes overhead
			v1Overhead := 4

			// V2 (varint): variable overhead
			v2Overhead := varintSize16(tc.keySize) + varintSize16(tc.valueSize)

			savings := v1Overhead - v2Overhead
			savingsPercent := float64(savings) / float64(v1Overhead+int(tc.keySize)+int(tc.valueSize)) * 100

			t.Logf("Key=%d Value=%d: V1 overhead=%d bytes, V2 overhead=%d bytes, Savings=%d bytes (%.2f%%)",
				tc.keySize, tc.valueSize, v1Overhead, v2Overhead, savings, savingsPercent)

			// For keys/values < 128, we should save 2 bytes
			if tc.keySize < 128 && tc.valueSize < 128 {
				if savings != 2 {
					t.Errorf("Expected 2 bytes savings for small keys, got %d", savings)
				}
			}
		})
	}
}

func BenchmarkVarintEncoding(b *testing.B) {
	buf := make([]byte, 10)
	value := uint16(12345)

	b.Run("Encode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			putUvarint16(buf, value)
		}
	})

	b.Run("Decode", func(b *testing.B) {
		putUvarint16(buf, value)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			uvarint16(buf)
		}
	})
}
