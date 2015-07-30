package backup

import (
	"bufio"
	"crypto/sha1"
	"io"

	"github.com/twcclan/goback/proto"

	"camlistore.org/pkg/rollsum"
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
