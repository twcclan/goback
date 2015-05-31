package backup

import (
	"bufio"
	"crypto/sha1"
	"io"

	"github.com/twcclan/goback/proto"

	"camlistore.org/pkg/rollsum"
)

const (
	// maxBlobSize is the largest blob we ever make when cutting up
	// a file.
	maxBlobSize = 1 << 20

	// bufioReaderSize is an explicit size for our bufio.Reader,
	// so we don't rely on NewReader's implicit size.
	// We care about the buffer size because it affects how far
	// in advance we can detect EOF from an io.Reader that doesn't
	// know its size.  Detecting an EOF bufioReaderSize bytes early
	// means we can plan for the final chunk.
	bufioReaderSize = 32 << 10

	// tooSmallThreshold is the threshold at which rolling checksum
	// boundaries are ignored if the current chunk being built is
	// smaller than this.
	tooSmallThreshold = 64 << 10

	inFlightChunks = 1
)

// copied from camlistore
type noteEOFReader struct {
	r      io.Reader
	sawEOF bool
}

func (r *noteEOFReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	if err == io.EOF {
		r.sawEOF = true
	}
	return
}

func Split(input io.Reader) <-chan *proto.Chunk {
	chunkChan := make(chan *proto.Chunk, inFlightChunks)
	go splitRoutine(input, chunkChan)

	return chunkChan
}

func splitRoutine(input io.Reader, chunkChan chan<- *proto.Chunk) {
	//close the channel when done
	defer close(chunkChan)

	rs := rollsum.New()

	noteEOF := &noteEOFReader{r: input}
	reader := bufio.NewReaderSize(noteEOF, bufioReaderSize)
	blobSize := 0
	var buf [maxBlobSize]byte

	//copy the chunk and send it via our channel
	split := func() {
		chunkBytes := make([]byte, blobSize)
		copy(chunkBytes, buf[:blobSize])
		sum := sha1.Sum(chunkBytes)

		chunkChan <- &proto.Chunk{
			Ref: &proto.ChunkRef{
				Sum: sum[:],
			},
			Data: chunkBytes,
		}
		blobSize = 0
	}

	//look for chunk borders to split the file at
	for b, err := reader.ReadByte(); err == nil; b, err = reader.ReadByte() {
		rs.Roll(b)
		buf[blobSize] = b
		blobSize++

		onSplit := rs.OnSplit()

		//split if we found a border, or we reached maxBlobSize
		if (onSplit && blobSize > tooSmallThreshold && !noteEOF.sawEOF) || blobSize == maxBlobSize {
			split()
		}
	}

	//create a chunk for any remaining data
	if blobSize > 0 {
		split()
	}
}
