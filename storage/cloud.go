package storage

import (
	"context"
	"fmt"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/twcclan/goback/storage/pack"

	"github.com/pkg/errors"
)

const blobObjectKey = "pack/%s/%s" // pack/<extension>/<filename>

var _ io.ReadSeeker = (*cloudFile)(nil)
var _ io.WriterTo = (*cloudFile)(nil)
var _ io.ReaderAt = (*cloudFile)(nil)
var _ os.FileInfo = (*cloudFileInfo)(nil)
var _ pack.ArchiveStorage = (*cloudStore)(nil)

func (s *cloudFile) Read(buf []byte) (int, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.readOnly {
		return 0, errors.New("Read only supported for readonly files")
	}

	n, err := s.ReadAt(buf, s.offset)
	s.offset += int64(n)

	return n, err
}

func (s *cloudFile) ReadAt(buf []byte, offset int64) (int, error) {
	if !s.readOnly {
		return 0, errors.New("Read only supported for readonly files")
	}

	if offset >= s.attrs.Size {
		return 0, io.EOF
	}

	length := int64(len(buf))

	if offset+length >= s.attrs.Size {
		length = s.attrs.Size - offset
	}

	reader, err := s.bucket.NewRangeReader(context.Background(), s.key, offset, length, nil)
	if err != nil {
		return 0, err
	}

	defer reader.Close()

	return io.ReadFull(reader, buf[:length])
}

func (s *cloudFile) WriteTo(w io.Writer) (int64, error) {
	if !s.readOnly {
		return -1, errors.New("WriteTo only supported for readonly files")
	}

	reader, err := s.bucket.NewReader(context.Background(), s.key, nil)

	if err != nil {
		return 0, err
	}

	defer reader.Close()

	return io.Copy(w, reader)
}

func (s *cloudFile) Seek(offset int64, whence int) (int64, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.readOnly {
		return -1, errors.New("Seek only supported for readonly files")
	}

	switch whence {
	case io.SeekStart:
		s.offset = offset
	case io.SeekCurrent:
		s.offset += offset
	case io.SeekEnd:
		s.offset = s.attrs.Size - offset
	default:
		return 0, errors.New("invalid whence value")
	}

	return s.offset, nil
}

type cloudFile struct {
	bucket  *blob.Bucket
	key     string
	onClose func()

	writer *blob.Writer

	readOnly bool
	offset   int64
	attrs    *blob.Attributes
	mtx      sync.Mutex
}

func (s *cloudFile) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.readOnly {
		// this is a nop for read-only files
		return nil
	}

	err := s.writer.Close()
	if err != nil {
		return err
	}
	s.writer = nil
	if s.onClose != nil {
		s.onClose()
	}

	return nil
}

func (s *cloudFile) Write(buf []byte) (int, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.writer.Write(buf)
}

func (s *cloudFile) Stat() (os.FileInfo, error) {
	return &cloudFileInfo{attrs: s.attrs, key: s.key}, nil
}

var _ pack.File = (*cloudFile)(nil)

type cloudStore struct {
	bucket *blob.Bucket

	openFilesMtx sync.Mutex
	openFiles    map[string]*cloudFile
}

func (c *cloudStore) openGCSFile(key string) (pack.File, error) {
	gcsLogger.WithField("key", key).Debug("Opening file")
	// if this is a file we are currently uploading
	// return the active instance instead
	c.openFilesMtx.Lock()
	file, ok := c.openFiles[key]
	c.openFilesMtx.Unlock()

	if ok {
		return file, nil
	}

	// request information about the file
	attrs, err := c.bucket.Attributes(context.Background(), key)
	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, pack.ErrFileNotFound
		}
		return nil, err
	}

	return &cloudFile{
		key:      key,
		attrs:    attrs,
		bucket:   c.bucket,
		readOnly: true,
	}, nil
}

func (c *cloudStore) newGCSFile(key string) (pack.File, error) {
	writer, err := c.bucket.NewWriter(context.Background(), key, nil)
	if err != nil {
		return nil, err
	}

	file := &cloudFile{
		writer: writer,
		bucket: c.bucket,
		key:    key,
		onClose: func() {
			c.openFilesMtx.Lock()
			delete(c.openFiles, key)
			c.openFilesMtx.Unlock()
		},
	}

	c.openFilesMtx.Lock()
	c.openFiles[key] = file
	c.openFilesMtx.Unlock()

	return file, nil
}

func (c *cloudStore) key(name string) string {
	return fmt.Sprintf(blobObjectKey, path.Ext(name), name)
}

func (c *cloudStore) Open(name string) (pack.File, error) {
	return c.openGCSFile(c.key(name))
}

func (c *cloudStore) Create(name string) (pack.File, error) {
	return c.newGCSFile(c.key(name))
}

func (c *cloudStore) Delete(name string) error {
	return c.bucket.Delete(context.Background(), c.key(name))
}

func (c *cloudStore) List() ([]string, error) {
	iter := c.bucket.List(&blob.ListOptions{
		Prefix:    fmt.Sprintf(blobObjectKey, pack.ArchiveSuffix, ""),
		Delimiter: "/",
	})

	var names []string

	for {
		attrs, err := iter.Next(context.Background())
		if err != nil {
			if err == io.EOF {
				break
			}

			return names, err
		}

		name := strings.TrimSuffix(path.Base(attrs.Key), path.Ext(attrs.Key))
		names = append(names, name)
	}

	return names, nil
}

type cloudFileInfo struct {
	key   string
	attrs *blob.Attributes
}

func (s *cloudFileInfo) Sys() interface{} {
	return s.attrs
}

func (s *cloudFileInfo) Size() int64 {
	return s.attrs.Size
}

func (s *cloudFileInfo) Name() string {
	return path.Base(s.key)
}

func (s *cloudFileInfo) Mode() os.FileMode {
	return 0
}

func (s *cloudFileInfo) ModTime() time.Time {
	return s.attrs.ModTime
}

func (s *cloudFileInfo) IsDir() bool {
	return false
}

func NewCloudStore(bucket *blob.Bucket) *cloudStore {
	storage := &cloudStore{
		bucket:    bucket,
		openFiles: make(map[string]*cloudFile),
	}

	return storage
}

//func NewCloudObjectStore(bucket, indexDir, cacheDir string) (*pack.PackStorage, error) {
//	credentials, err := google.FindDefaultCredentials(context.Background(), storage.ScopeReadWrite)
//	if err != nil {
//		return nil, err
//	}
//
//	client, err := storage.NewClient(context.Background(), option.WithCredentials(credentials))
//	if err != nil {
//		return nil, err
//	}
//
//	storage := &cloudStore{
//		bucket:    bucket,
//		gcs:       client,
//		openFiles: make(map[string]*cloudFile),
//	}
//
//	err = os.MkdirAll(indexDir, 0644)
//	if err != nil {
//		return nil, err
//	}
//
//	idx, err := badgerIdx.NewBadgerIndex(indexDir)
//
//	options := []pack.PackOption{
//		pack.WithArchiveStorage(storage),
//		pack.WithArchiveIndex(idx),
//		pack.WithMaxParallel(64),
//		pack.WithCloseBeforeRead(true),
//		pack.WithMaxSize(1024 * 1024 * 1024),
//		pack.WithCompaction(pack.CompactionConfig{
//			Periodically:      24 * time.Hour,
//			MinimumCandidates: 1000,
//			GarbageCollection: false,
//			OnOpen:            false,
//		}),
//	}
//
//	if cacheDir != "" {
//		err = os.MkdirAll(cacheDir, 0644)
//		if err != nil {
//			return nil, err
//		}
//
//		cache, err := badger.New(cacheDir)
//		if err != nil {
//			return nil, err
//		}
//
//		options = append(options, pack.WithMetadataCache(cache))
//	}
//
//	return pack.NewPackStorage(options...)
//}
