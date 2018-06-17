package backup

import (
	"context"
	"io"
	"log"
	"sort"
	"sync/atomic"

	"github.com/twcclan/goback/proto"

	"camlistore.org/pkg/rollsum"
	"go4.org/syncutil"
	"golang.org/x/sync/errgroup"
)

const (
	// maxBlobSize is the largest blob we ever make when cutting up
	// a file.
	maxBlobSize = 1 << 20

	// maxFileParts is the number of file parts at which we start
	// splitting the file object
	maxFileParts = 25000

	// tooSmallThreshold is the threshold at which rolling checksum
	// boundaries are ignored if the current chunk being built is
	// smaller than this.
	tooSmallThreshold = 0 //64 << 10

	inFlightChunks = 80
)

func newFileWriter(ctx context.Context, store ObjectStore) *fileWriter {
	return &fileWriter{
		store:            store,
		rs:               rollsum.New(),
		parts:            make([]*proto.FilePart, 0),
		storageErr:       new(atomic.Value),
		storageSemaphore: syncutil.NewGate(inFlightChunks),
		ctx:              ctx,
	}
}

type fileWriter struct {
	store            ObjectStore
	buf              [maxBlobSize]byte
	blobSize         int64
	offset           int64
	rs               *rollsum.RollSum
	parts            []*proto.FilePart
	storageErr       *atomic.Value
	storageGroup     syncutil.Group
	storageSemaphore *syncutil.Gate
	ref              *proto.Ref
	ctx              context.Context

	pending int32
}

func (bfw *fileWriter) uploader() {
	for {
		select {}
	}
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
		defer bfw.storageSemaphore.Done()
		defer atomic.AddInt32(&bfw.pending, -1)

		has, err := bfw.store.Has(bfw.ctx, blob.Ref())
		if has {
			return nil
		}

		err = bfw.store.Put(bfw.ctx, blob)
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

	var file *proto.Object

	if len(bfw.parts) > maxFileParts {
		var splits []*proto.Ref
		for len(bfw.parts) > 0 {
			max := maxFileParts
			if max > len(bfw.parts) {
				max = len(bfw.parts)
			}

			split := proto.NewObject(&proto.File{Parts: bfw.parts[:max]})

			err := bfw.store.Put(bfw.ctx, split)
			if err != nil {
				return err
			}

			bfw.parts = bfw.parts[max:]
			splits = append(splits, split.Ref())
		}

		file = proto.NewObject(&proto.File{
			Splits: splits,
		})
	} else {
		file = proto.NewObject(&proto.File{
			Parts: bfw.parts,
		})
	}

	bfw.ref = file.Ref()

	if err = bfw.store.Put(bfw.ctx, file); err != nil {
		return
	}

	// wait for all chunk uploads to finish
	return bfw.storageGroup.Err()
}

var _ io.WriteCloser = new(fileWriter)

func newFileReader(ctx context.Context, store ObjectStore, file *proto.File) *fileReader {
	return &fileReader{
		store: store,
		file:  file,
		ctx:   ctx,
	}
}

type fileReader struct {
	store     ObjectStore
	file      *proto.File
	parts     []*proto.FilePart
	blob      *proto.Object
	partIndex int
	offset    int64
	ctx       context.Context
}

type searchCallback func(index int) bool

func (bfr *fileReader) search(index int) bool {
	part := bfr.parts[index]

	return bfr.offset <= int64(part.Offset+part.Length-1)
}

func (bfr *fileReader) size() int64 {
	parts := bfr.parts
	length := len(parts)
	if length > 0 {
		last := parts[length-1]
		return int64(last.Offset + last.Length)
	}

	return 0
}

type partResponse struct {
	index int
	blob  *proto.Blob
}

type partRequest struct {
	index int
	part  *proto.FilePart
}

func (bfr *fileReader) getFileParts(ctx context.Context) ([]*proto.FilePart, error) {
	if bfr.parts == nil {
		// this is a large file so we need to fetch the referenced file objects
		if bfr.file.Splits != nil {
			subFiles := make([]*proto.File, len(bfr.file.Splits))
			grp, grpCtx := errgroup.WithContext(ctx)

			for i := range bfr.file.Splits {
				index := i

				// get all parts in parallel, can add bounds later if required
				grp.Go(func() error {
					ref := bfr.file.Splits[index]

					obj, err := bfr.store.Get(grpCtx, ref)
					if err != nil {
						return err
					}

					subFiles[index] = obj.GetFile()
					return nil
				})
			}

			err := grp.Wait()
			if err != nil {
				return nil, err
			}

			for _, subFile := range subFiles {
				bfr.parts = append(bfr.parts, subFile.GetParts()...)
			}
		} else {
			bfr.parts = bfr.file.Parts
		}
	}

	return bfr.parts, nil
}

func (bfr *fileReader) WriteTo(writer io.Writer) (int64, error) {
	group, ctx := errgroup.WithContext(bfr.ctx)
	requests := make(chan partRequest)
	parts := make(chan partResponse)
	numWorker := 512
	bytesWritten := int64(0)

	fileParts, err := bfr.getFileParts(ctx)
	if err != nil {
		return 0, err
	}

	numParts := len(fileParts)

	// writer goroutine
	group.Go(func() error {
		stash := make(map[int]*proto.Blob)

		for partIndex := 0; partIndex < numParts; {
			// check if we have the next part stashed already
			if p, ok := stash[partIndex]; ok {
				n, err := writer.Write(p.Data)
				bytesWritten += int64(n)

				if err != nil {
					return err
				}

				delete(stash, partIndex)
				partIndex++
				continue
			}

			select {
			case <-ctx.Done():
				return nil
			case p := <-parts:
				// stash it for later
				stash[p.index] = p.blob
			}
		}

		return nil
	})

	// scheduler goroutine
	group.Go(func() error {
		defer func() {
			close(requests)
		}()

		// push all parts to the request channel
		for i, part := range fileParts {
			select {
			// cancelling the context is the only way this should ever exit early
			case <-ctx.Done():
				return ctx.Err()
			case requests <- partRequest{index: i, part: part}:
			}
		}

		return nil
	})

	// start workers
	for i := 0; i < numWorker; i++ {
		group.Go(func() error {
			for req := range requests {
				obj, err := bfr.store.Get(bfr.ctx, req.part.Ref)
				// TODO: implement retry here
				if err != nil {
					return err
				}

				if obj == nil {
					log.Printf("Warning: couldn't find part %d (%x) of file %X", req.index, req.part.Ref.Sha1, proto.NewObject(bfr.file).Ref().Sha1)
					obj = proto.NewObject(&proto.Blob{
						Data: make([]byte, req.part.Length),
					})
				}

				parts <- partResponse{index: req.index, blob: obj.GetBlob()}
			}

			return nil
		})
	}

	return bytesWritten, group.Wait()
}

func (bfr *fileReader) Read(b []byte) (n int, err error) {
	fileParts, err := bfr.getFileParts(bfr.ctx)
	if err != nil {
		return 0, err
	}

	// check if we reached EOF

	if bfr.offset >= bfr.size() {
		return 0, io.EOF
	}

	// the number of bytes requested by the caller
	// we return fewer bytes if we hit a chunk border
	n = len(b)

	// the chunk that represents the currently active part
	// of the file that is being read
	part := fileParts[bfr.partIndex]

	// calculate the offset for the currently active chunk
	relativeOffset := bfr.offset - int64(part.Offset)

	// calculate the bytes left for reading in this file part

	bytesRemaining := int64(part.Length) - relativeOffset

	// exit early if no buffer was provided
	if n == 0 {
		return 0, ErrEmptyBuffer
	}

	// limit the number of bytes read
	// so we don't cross chunk borders
	if n > int(bytesRemaining) {
		n = int(bytesRemaining)
	}

	// lazily load blob from our object store
	if bfr.blob == nil {
		bfr.blob, err = bfr.store.Get(bfr.ctx, part.Ref)
	}

	if err != nil {
		return
	}

	// this means we have a corrupt backup :(
	if bfr.blob == nil {
		// fake some data for testing
		log.Printf("Warning: couldn't find part %d (%x) of file %X", bfr.partIndex, part.Ref.Sha1, proto.NewObject(bfr.file).Ref().Sha1)
		bfr.blob = proto.NewObject(&proto.Blob{
			Data: make([]byte, bytesRemaining),
		})
	}

	// read data from chunk
	copy(b, bfr.blob.GetBlob().Data[relativeOffset:relativeOffset+int64(n)])

	// if we reached the end of the current chunk
	// we'll unload it and increase the part index
	if relativeOffset+int64(n) == int64(part.Length) {
		bfr.blob = nil
		bfr.partIndex++
	}

	// increase offset
	bfr.offset += int64(n)

	return
}

func (bfr *fileReader) Seek(offset int64, whence int) (int64, error) {
	fileParts, err := bfr.getFileParts(bfr.ctx)
	if err != nil {
		return 0, err
	}

	switch whence {
	case io.SeekStart:
		bfr.offset = offset
	case io.SeekCurrent:
		bfr.offset += offset
	case io.SeekEnd:
		bfr.offset = bfr.size() - 1 - offset
	}

	if bfr.offset < 0 || bfr.offset > bfr.size() {
		return bfr.offset, ErrIllegalOffset
	}

	// find the part that contains the requested data
	if i := sort.Search(len(fileParts), bfr.search); i != bfr.partIndex {
		// if it is not the currently active part
		// unload the currently loaded chunk
		bfr.partIndex = i
		bfr.blob = nil
	}

	return bfr.offset, nil
}

var _ io.ReadSeeker = (*fileReader)(nil)
