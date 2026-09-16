// Package chunker cuts byte streams into content-defined chunks with a
// FastCDC-style normalised Gear hash.
//
// The profile is fixed so that every agent cuts identical bytes identically:
// minimum 16 KiB, target 64 KiB, maximum 256 KiB. The gear table entry for
// byte value v is the first eight bytes, big-endian, of SHA-256 of the single
// byte v. The masks have 17 (below the target) and 15 (above it) one bits at
// every third position from the top of the 64-bit fingerprint.
package chunker

import (
	"crypto/sha256"
	"encoding/binary"
)

const (
	// MinSize is the smallest chunk cut before the end of a stream.
	MinSize = 16 << 10
	// NormalSize is the target chunk size.
	NormalSize = 64 << 10
	// MaxSize is the largest chunk; a cut is forced there.
	MaxSize = 256 << 10
)

var (
	gear  [256]uint64
	maskS = spreadMask(17)
	maskL = spreadMask(15)
)

func init() {
	for v := 0; v < 256; v++ {
		sum := sha256.Sum256([]byte{byte(v)})
		gear[v] = binary.BigEndian.Uint64(sum[:8])
	}
}

func spreadMask(ones int) uint64 {
	var mask uint64
	for j := 0; j < ones; j++ {
		mask |= 1 << uint(63-3*j)
	}

	return mask
}

// FastCDC holds the fingerprint of the chunk currently being scanned.
type FastCDC struct {
	fp uint64
}

// Scan looks at chunk[scanned:], where chunk holds the bytes of the current
// chunk collected so far, and returns the chunk length if a cut point was
// found, or 0 if more bytes are needed. A cut resets the fingerprint.
func (c *FastCDC) Scan(chunk []byte, scanned int) int {
	i := scanned
	if i < MinSize {
		i = MinSize
	}

	for ; i < len(chunk); i++ {
		c.fp = c.fp<<1 + gear[chunk[i]]

		mask := maskL
		if i < NormalSize {
			mask = maskS
		}

		if c.fp&mask == 0 {
			c.fp = 0
			return i
		}

		if i+1 >= MaxSize {
			c.fp = 0
			return i + 1
		}
	}

	return 0
}

// Reset forgets the current chunk, for use when the stream ends mid-chunk.
func (c *FastCDC) Reset() {
	c.fp = 0
}

// Split cuts a whole buffer and returns the chunk lengths in order.
func Split(data []byte) []int {
	var (
		c       FastCDC
		lengths []int
	)

	for len(data) > 0 {
		n := c.Scan(data, 0)
		if n == 0 {
			n = len(data)
			c.Reset()
		}

		lengths = append(lengths, n)
		data = data[n:]
	}

	return lengths
}
