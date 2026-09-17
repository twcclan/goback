package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	badgerIdx "github.com/twcclan/goback/index/badger"
	"github.com/twcclan/goback/storage/badger"
	"github.com/twcclan/goback/storage/pack"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

const (
	blobObjectPrefix = "pack/"
	blobObjectKey    = blobObjectPrefix + "%s/%s" // pack/<extension>/<filename>
)

var bucketLogger = logrus.WithField("storage", "cloud")

var _ io.ReadSeeker = (*bucketFile)(nil)
var _ io.WriterTo = (*bucketFile)(nil)
var _ io.ReaderAt = (*bucketFile)(nil)
var _ os.FileInfo = (*bucketFileInfo)(nil)
var _ pack.ArchiveStorage = (*BucketStore)(nil)

func (s *bucketFile) Read(buf []byte) (int, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.readOnly {
		return 0, errors.New("Read only supported for readonly files")
	}

	n, err := s.ReadAt(buf, s.offset)
	s.offset += int64(n)

	return n, err
}

func (s *bucketFile) ReadAt(buf []byte, offset int64) (int, error) {
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

func (s *bucketFile) WriteTo(w io.Writer) (int64, error) {
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

func (s *bucketFile) Seek(offset int64, whence int) (int64, error) {
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

type bucketFile struct {
	bucket  *blob.Bucket
	key     string
	onClose func()

	writer *blob.Writer

	readOnly bool
	offset   int64
	attrs    *blob.Attributes
	mtx      sync.Mutex
}

func (s *bucketFile) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.readOnly || s.writer == nil {
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

func (s *bucketFile) Write(buf []byte) (int, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.writer.Write(buf)
}

func (s *bucketFile) Stat() (os.FileInfo, error) {
	return &bucketFileInfo{attrs: s.attrs, key: s.key}, nil
}

var _ pack.File = (*bucketFile)(nil)

type BucketStore struct {
	bucket *blob.Bucket

	openFilesMtx sync.Mutex
	openFiles    map[string]*bucketFile
}

func (c *BucketStore) openFile(key string) (pack.File, error) {
	bucketLogger.WithField("key", key).Debug("Opening file")
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

	return &bucketFile{
		key:      key,
		attrs:    attrs,
		bucket:   c.bucket,
		readOnly: true,
	}, nil
}

func (c *BucketStore) newGCSFile(key string) (pack.File, error) {
	writer, err := c.bucket.NewWriter(context.Background(), key, nil)
	if err != nil {
		return nil, err
	}

	file := &bucketFile{
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

func (c *BucketStore) key(name string) string {
	return fmt.Sprintf(blobObjectKey, path.Ext(name), name)
}

func (c *BucketStore) Open(name string) (pack.File, error) {
	return c.openFile(c.key(name))
}

func (c *BucketStore) Create(name string) (pack.File, error) {
	return c.newGCSFile(c.key(name))
}

func (c *BucketStore) Delete(name string) error {
	return c.bucket.Delete(context.Background(), c.key(name))
}

func (c *BucketStore) DeleteAll() error {
	iter := c.bucket.List(&blob.ListOptions{
		Prefix: blobObjectPrefix,
	})

	for {
		attrs, err := iter.Next(context.Background())
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if attrs.IsDir {
			continue
		}

		err = c.bucket.Delete(context.Background(), attrs.Key)
		if err != nil {
			return err
		}
	}
}

func (c *BucketStore) List(extension string) ([]string, error) {
	prefix := blobObjectPrefix
	if extension != "" {
		if len(extension) < 2 || extension[0] != '.' {
			return nil, pack.ErrInvalidExtension
		}

		prefix = fmt.Sprintf(blobObjectKey, extension, "")
	}

	iter := c.bucket.List(&blob.ListOptions{
		Prefix: prefix,
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

		if attrs.IsDir {
			continue
		}

		// keys are pack/<extension>/<name>; the name may hold slashes
		name := strings.TrimPrefix(attrs.Key, blobObjectPrefix)
		if _, rest, ok := strings.Cut(name, "/"); ok && extension == "" {
			name = rest
		} else if extension != "" {
			name = strings.TrimPrefix(attrs.Key, prefix)
		}

		names = append(names, name)
	}

	return names, nil
}

type bucketFileInfo struct {
	key   string
	attrs *blob.Attributes
}

func (s *bucketFileInfo) Sys() interface{} {
	return s.attrs
}

func (s *bucketFileInfo) Size() int64 {
	return s.attrs.Size
}

func (s *bucketFileInfo) Name() string {
	return path.Base(s.key)
}

func (s *bucketFileInfo) Mode() os.FileMode {
	return 0
}

func (s *bucketFileInfo) ModTime() time.Time {
	return s.attrs.ModTime
}

func (s *bucketFileInfo) IsDir() bool {
	return false
}

func NewBucketStore(bucket *blob.Bucket) *BucketStore {
	storage := &BucketStore{
		bucket:    bucket,
		openFiles: make(map[string]*bucketFile),
	}

	return storage
}

// NewBucketObjectStore returns a pack store over a remote bucket, with a local
// badger archive index at indexDir and, if cacheDir is not empty, a local
// metadata cache; extra pack options follow.
func NewBucketObjectStore(bucket *blob.Bucket, indexDir, cacheDir string, extra ...pack.PackOption) (backup.ObjectStore, error) {
	err := os.MkdirAll(indexDir, 0755)
	if err != nil {
		return nil, err
	}

	idx, err := badgerIdx.NewBadgerIndex(indexDir)
	if err != nil {
		return nil, err
	}

	options := []pack.PackOption{
		pack.WithArchiveStorage(NewBucketStore(bucket)),
		pack.WithArchiveIndex(idx),
		pack.WithMaxParallel(64),
		pack.WithCloseBeforeRead(true),
		pack.WithMaxSize(1024 * 1024 * 1024),
		pack.WithIdleFinalize(5 * time.Minute),
		pack.WithSessionLease(30 * time.Minute),
		pack.WithCompaction(pack.CompactionConfig{
			Periodically:      24 * time.Hour,
			MinimumCandidates: 1000,
		}),
	}

	if cacheDir != "" {
		err = os.MkdirAll(cacheDir, 0755)
		if err != nil {
			return nil, err
		}

		cache, err := badger.New(cacheDir)
		if err != nil {
			return nil, err
		}

		options = append(options, pack.WithMetadataCache(cache))
	}

	return pack.NewPackStorage(append(options, extra...)...)
}
