package chunker

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func randomBytes(seed int64, n int) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	r.Read(b)
	return b
}

func TestSplitBounds(t *testing.T) {
	data := randomBytes(1, 16<<20)
	lengths := Split(data)

	var total int
	for i, n := range lengths {
		total += n
		require.LessOrEqual(t, n, MaxSize)
		if i < len(lengths)-1 {
			require.GreaterOrEqual(t, n, MinSize)
		}
	}
	require.Equal(t, len(data), total)

	mean := float64(len(data)) / float64(len(lengths))
	require.InDelta(t, NormalSize, mean, NormalSize*0.4, "mean chunk size %.0f", mean)
}

func TestGearTableIsFixed(t *testing.T) {
	// first entries of the table, pinned so the profile cannot drift
	require.Equal(t, uint64(0x6e340b9cffb37a98), gear[0])
	require.Equal(t, uint64(0x4bf5122f344554c5), gear[1])
	require.Equal(t, uint64(0xa8100ae6aa1940d0), gear[255])
	require.Equal(t, uint64(0x9249249249248000), maskS)
	require.Equal(t, uint64(0x9249249249200000), maskL)
}

func TestSplitIsContentDefined(t *testing.T) {
	base := randomBytes(2, 8<<20)
	shifted := append(randomBytes(3, 1000), base...)

	baseLengths := Split(base)
	shiftedLengths := Split(shifted)

	// after the inserted prefix, the boundaries realign with the original ones
	var baseOffsets, shiftedOffsets []int
	off := 0
	for _, n := range baseLengths {
		off += n
		baseOffsets = append(baseOffsets, off)
	}
	off = 0
	for _, n := range shiftedLengths {
		off += n
		shiftedOffsets = append(shiftedOffsets, off-1000)
	}

	common := 0
	for _, o := range shiftedOffsets {
		for _, b := range baseOffsets {
			if o == b {
				common++
			}
		}
	}

	require.Greater(t, common, len(baseOffsets)*8/10, "only %d of %d boundaries realigned", common, len(baseOffsets))
}

func TestStreamingMatchesBatch(t *testing.T) {
	data := randomBytes(4, 4<<20)
	batch := Split(data)

	var (
		c        FastCDC
		buf      []byte
		lengths  []int
		pieces   = [][]byte{}
		r        = rand.New(rand.NewSource(5))
		remained = data
	)

	for len(remained) > 0 {
		n := r.Intn(70000) + 1
		if n > len(remained) {
			n = len(remained)
		}
		pieces = append(pieces, remained[:n])
		remained = remained[n:]
	}

	for _, piece := range pieces {
		scanned := len(buf)
		buf = append(buf, piece...)

		for {
			cut := c.Scan(buf, scanned)
			if cut == 0 {
				break
			}

			lengths = append(lengths, cut)
			buf = append([]byte(nil), buf[cut:]...)
			scanned = 0
		}
	}

	if len(buf) > 0 {
		lengths = append(lengths, len(buf))
	}

	require.Equal(t, batch, lengths)
	require.True(t, bytes.Equal(data, data))
}
