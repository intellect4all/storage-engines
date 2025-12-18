package btree

import "errors"

// Variable-length integer encoding (similar to Protocol Buffers varint)
// Reduces space overhead for small values:
//   Values 0-127:        1 byte  (7 bits + continuation bit)
//   Values 128-16383:    2 bytes (14 bits + continuation bits)
//   Values 16384-2097151: 3 bytes (21 bits + continuation bits)
//
// This is more space-efficient than fixed 2-byte encoding for most keys/values

var (
	ErrVarintOverflow = errors.New("varint overflow")
	ErrVarintTrunc    = errors.New("varint truncated")
)

// putUvarint encodes a uint64 into buf and returns the number of bytes written.
// If the buffer is too small, putUvarint will panic.
func putUvarint(buf []byte, x uint64) int {
	i := 0
	for x >= 0x80 {
		buf[i] = byte(x) | 0x80
		x >>= 7
		i++
	}
	buf[i] = byte(x)
	return i + 1
}

// uvarint decodes a uint64 from buf and returns that value and the
// number of bytes read (> 0). If an error occurred, the value is 0
// and the number of bytes n is <= 0.
func uvarint(buf []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, b := range buf {
		if i == 9 {
			// Overflow - varint too long
			return 0, -(i + 1)
		}
		if b < 0x80 {
			if i == 9-1 && b > 1 {
				// Overflow
				return 0, -(i + 1)
			}
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0
}

// varintSize returns the number of bytes needed to encode x as a varint
func varintSize(x uint64) int {
	n := 0
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}

// For convenience, define functions for uint16 (most common case)

// putUvarint16 encodes a uint16 as varint
func putUvarint16(buf []byte, x uint16) int {
	return putUvarint(buf, uint64(x))
}

// uvarint16 decodes a uint16 from varint
func uvarint16(buf []byte) (uint16, int) {
	x, n := uvarint(buf)
	if n <= 0 {
		return 0, n
	}
	if x > 0xFFFF {
		return 0, -1 // Overflow for uint16
	}
	return uint16(x), n
}

// varintSize16 returns the number of bytes needed to encode x as a varint
func varintSize16(x uint16) int {
	if x < 128 {
		return 1
	}
	if x < 16384 {
		return 2
	}
	return 3
}
