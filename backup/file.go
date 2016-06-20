package backup

import (
	"io"
	"os"
	"sync/atomic"

	"github.com/twcclan/goback/proto"

	"camlistore.org/pkg/rollsum"
	"go4.org/syncutil"
)

const (
	// maxBlobSize is the largest blob we ever make when cutting up
	// a file.
	maxBlobSize = 1 << 20

	// tooSmallThreshold is the threshold at which rolling checksum
	// boundaries are ignored if the current chunk being built is
	// smaller than this.
	tooSmallThreshold = 0 //64 << 10

	inFlightChunks = 128
)

type fileWriter struct {
	store            ObjectStore
	buf              [maxBlobSize]byte
	blobSize         int64
	offset           int64
	rs               *rollsum.RollSum
	parts            []*proto.FilePart
	info             os.FileInfo
	storageErr       *atomic.Value
	storageGroup     syncutil.Group
	storageSemaphore *syncutil.Gate
	ref              *proto.Ref

	pending int32
}

func (bfw *fileWriter) split() {
	chunkBytes := make([]byte, bfw.blobSize)
	copy(chunkBytes, bfw.buf[:bfw.blobSize])

	blob := proto.NewObject(&proto.Blob{
		Data: chunkBytes,
	})

	length := bfw.blobSize
	bfw.blobSize = 0

	bfw.parts = append(bfw.parts, &proto.FilePart{
		Ref:    blob.Ref(),
		Offset: uint64(bfw.offset),
		Length: uint64(length),
	})

	bfw.offset += length

	bfw.storageSemaphore.Start()
	atomic.AddInt32(&bfw.pending, 1)

	bfw.storageGroup.Go(func() error {
		err := bfw.store.Put(blob)
		bfw.storageSemaphore.Done()
		defer atomic.AddInt32(&bfw.pending, -1)
		if err != nil {
			// store the error here so future calls to write can exit early
			bfw.storageErr.Store(err)
		}
		return err
	})
}

func (bfw *fileWriter) Ref() *proto.Ref {
	return bfw.ref
}

func (bfw *fileWriter) Write(bytes []byte) (int, error) {
	deferred := bfw.storageErr.Load()

	if deferred != nil {
		return 0, deferred.(error)
	}

	for _, b := range bytes {
		bfw.rs.Roll(b)
		bfw.buf[bfw.blobSize] = b
		bfw.blobSize++

		onSplit := bfw.rs.OnSplit()

		//split if we found a border, or we reached maxBlobSize
		if (onSplit && bfw.blobSize > tooSmallThreshold) || bfw.blobSize == maxBlobSize {
			bfw.split()
		}
	}

	return len(bytes), nil
}

func (bfw *fileWriter) Close() (err error) {
	if bfw.blobSize > 0 {
		bfw.split()
	}

	// store the file -> chunks mapping
	file := proto.NewObject(&proto.File{
		Parts: bfw.parts,
	})

	bfw.ref = file.Ref()

	if err = bfw.store.Put(file); err != nil {
		return
	}

	// wait for all chunk uploads to finish
	return bfw.storageGroup.Err()
}

var _ io.WriteCloser = new(fileWriter)
