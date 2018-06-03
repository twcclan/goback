package pack

import (
	"bytes"
	"io"
	"testing"

	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twcclan/goback/proto"
)

func TestNewArchive(t *testing.T) {
	storage := &MockArchiveStorage{}

	// it should first create the new archive file and then open it for reading
	storage.On("Create", mock.AnythingOfType("string")).Return(nil, nil)
	storage.On("Open", mock.AnythingOfType("string")).Return(nil, nil)

	ar, err := newArchive(storage)
	require.Nil(t, err)
	assert.NotEmpty(t, ar.name)

	storage.AssertExpectations(t)
}

func TestOpenArchive(t *testing.T) {
	storage := &MockArchiveStorage{}
	archiveFile := &MockFile{}
	archiveSize := int64(12345)
	indexFile := &MockFile{}
	fileInfo := &mockFileInfo{}

	name := "test"
	idx := index{
		indexRecord{
			Length: 1,
			Offset: 2,
		},
	}
	idxBuffer := new(bytes.Buffer)
	_, err := idx.WriteTo(idxBuffer)
	require.Nil(t, err)

	// it should first open the archive for reading
	storage.On("Open", name+ArchiveSuffix).Return(archiveFile, nil).Once()
	archiveFile.On("Stat").Return(fileInfo, nil).Once()
	fileInfo.On("Size").Return(archiveSize).Once()

	// then it should open and read the index file
	storage.On("Open", name+IndexExt).Return(indexFile, nil)
	indexFile.On("Read", mock.Anything).Return(idxBuffer.Len(), io.EOF).Run(func(args mock.Arguments) {
		idxBuffer.Read(args.Get(0).([]byte))
	})
	indexFile.On("Close").Return(nil)

	ar, err := openArchive(storage, name)
	require.Nil(t, err)
	assert.Equal(t, name, ar.name)
	assert.EqualValues(t, archiveSize, ar.size)
	assert.Equal(t, idx, ar.readIndex)
	assert.Equal(t, archiveFile, ar.readFile)
	assert.Equal(t, nil, ar.writeFile)

	storage.AssertExpectations(t)
	archiveFile.AssertExpectations(t)
	indexFile.AssertExpectations(t)
	fileInfo.AssertExpectations(t)
}

func TestArchiveFile(t *testing.T) {
	buffer := new(bytes.Buffer)

	ar := &archive{
		writeIndex: make(map[string]*indexRecord),
	}

	objects := makeTestData(t, 10)

	t.Run("put", func(t *testing.T) {
		archiveFile := &MockFile{}
		ar.writeFile = archiveFile

		// pipe all writes into our buffer
		archiveFile.On("Write", mock.Anything).Return(func(buf []byte) int {
			n, _ := buffer.Write(buf)
			return n
		}, nil)

		for _, obj := range objects {
			assert.Nil(t, ar.Put(context.Background(), obj))
		}

		assert.Len(t, ar.writeIndex, 10)
		archiveFile.AssertExpectations(t)
	})

	t.Run("getReader", func(t *testing.T) {
		reader := bytes.NewReader(buffer.Bytes())
		archiveFile := &MockFile{}
		ar.readFile = archiveFile

		// iterate over the write index so we're getting random order
	outer:
		for hash, loc := range ar.writeIndex {
			// seek in our reader
			archiveFile.On("Seek", int64(loc.Offset), io.SeekStart).Return(func(offset int64, whence int) int64 {
				n, _ := reader.Seek(offset, whence)
				return n
			}, nil).Once()

			// read from our reader
			archiveFile.On("Read", mock.Anything).Return(func(buf []byte) int {
				n, _ := reader.Read(buf)
				return n
			}, nil)

			ref := &proto.Ref{
				Sha1: []byte(hash),
			}

			newObj, err := ar.getRaw(context.Background(), ref, loc)
			require.Nil(t, err)

			for _, obj := range objects {
				if assert.ObjectsAreEqual(obj.GetBlob().GetData(), newObj.GetBlob().GetData()) {
					continue outer
				}
			}

			t.Errorf("Didn't expect to see object %x", newObj.Ref())
		}

		archiveFile.AssertExpectations(t)
	})

	t.Run("getReaderAt", func(t *testing.T) {
		reader := bytes.NewReader(buffer.Bytes())
		readerAt := &mockReaderAt{}
		ar.readFile = readerAt

		// iterate over the write index so we're getting random order
	outer:
		for hash, loc := range ar.writeIndex {
			// read from our reader
			readerAt.On("ReadAt", mock.Anything, int64(loc.Offset)).Return(func(buf []byte, offset int64) int {
				n, _ := reader.ReadAt(buf, offset)
				return n
			}, nil)

			ref := &proto.Ref{
				Sha1: []byte(hash),
			}

			newObj, err := ar.getRaw(context.Background(), ref, loc)
			require.Nil(t, err)

			for _, obj := range objects {
				if assert.ObjectsAreEqual(obj.GetBlob().GetData(), newObj.GetBlob().GetData()) {
					continue outer
				}
			}

			t.Errorf("Didn't expect to see object %x", newObj.Ref())
		}

		readerAt.AssertExpectations(t)
	})

	t.Run("foreach", func(t *testing.T) {
		readerInput := func(t *testing.T, run func(file File)) {
			reader := bytes.NewReader(buffer.Bytes())
			archiveFile := &MockFile{}

			var readerN int
			var readerErr error
			archiveFile.On("Read", mock.Anything).Return(
				func(buf []byte) int {
					return readerN
				}, func(buf []byte) error {
					return readerErr
				}).Run(func(args mock.Arguments) {
				readerN, readerErr = reader.Read(args.Get(0).([]byte))
			})

			archiveFile.On("Close").Return(nil)

			defer archiveFile.AssertExpectations(t)

			run(archiveFile)
		}

		writerToInput := func(t *testing.T, run func(file File)) {
			archiveFile := &mockWriterTo{}

			var writerN int64
			var writerErr error
			archiveFile.On("WriteTo", mock.Anything).Return(
				func(_ io.Writer) int64 {
					return writerN
				}, func(_ io.Writer) error {
					return writerErr
				}).Run(func(args mock.Arguments) {
				writerN, writerErr = buffer.WriteTo(args.Get(0).(io.Writer))
			})

			archiveFile.On("Close").Return(nil)

			defer archiveFile.AssertExpectations(t)

			run(archiveFile)
		}

		runForeach := func(load bool, input func(*testing.T, func(file File))) func(*testing.T) {
			return func(t *testing.T) {

				storage := &MockArchiveStorage{}
				ar.storage = storage

				input(t, func(file File) {
					storage.On("Open", ar.archiveName()).Return(file, nil)

					pred := loadAll
					if !load {
						pred = loadNone
					}

					ar.foreach(pred, func(hdr *proto.ObjectHeader, bytes []byte, offset uint32, length uint32) error {
						loc, ok := ar.writeIndex[string(hdr.Ref.Sha1)]
						require.True(t, ok)

						assert.EqualValues(t, loc.Offset, offset)
						assert.EqualValues(t, loc.Length, length)

						if load {
							obj, err := proto.NewObjectFromCompressedBytes(bytes)
							require.Nil(t, err)
							assert.Equal(t, hdr.Ref, obj.Ref())
						}

						return nil
					})
				})
			}
		}

		t.Run("reader", func(t *testing.T) {
			t.Run("loadAll", runForeach(true, readerInput))
			t.Run("loadNone", runForeach(false, readerInput))
		})

		t.Run("writerTo", func(t *testing.T) {
			t.Run("loadAll", runForeach(true, writerToInput))
			t.Run("loadNone", runForeach(false, writerToInput))
		})
	})
}
