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
	"go.opentelemetry.io/otel/attribute"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

const (
	blobObjectPrefix = "pack/"
	blobObjectKey    = blobObjectPrefix + "%s/%s" // pack/<extension>/<filename>
)

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

	reader, err := s.store.bucket.NewRangeReader(context.Background(), s.key, offset, length, nil)
	if err != nil {
		s.store.count(OpGet, 0)

		return 0, err
	}

	defer reader.Close()

	n, err := io.ReadFull(reader, buf[:length])
	s.store.count(OpGet, int64(n))

	return n, err
}

func (s *bucketFile) WriteTo(w io.Writer) (int64, error) {
	if !s.readOnly {
		return -1, errors.New("WriteTo only supported for readonly files")
	}

	reader, err := s.store.bucket.NewReader(context.Background(), s.key, nil)

	if err != nil {
		s.store.count(OpGet, 0)

		return 0, err
	}

	defer reader.Close()

	n, err := io.Copy(w, reader)
	s.store.count(OpGet, n)

	return n, err
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
	store   *BucketStore
	key     string
	onClose func()

	writer  *blob.Writer
	written int64

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
	s.store.count(OpPut, s.written)

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

	n, err := s.writer.Write(buf)
	s.written += int64(n)

	return n, err
}

func (s *bucketFile) Stat() (os.FileInfo, error) {
	return &bucketFileInfo{attrs: s.attrs, key: s.key}, nil
}

var _ pack.File = (*bucketFile)(nil)

// BucketStore is a pack.ArchiveStorage over a gocloud bucket, keyed
// pack/<extension>/<name>.
type BucketStore struct {
	bucket *blob.Bucket
	attrs  []attribute.KeyValue

	openFilesMtx sync.Mutex
	openFiles    map[string]*bucketFile
}

// A BucketOption adjusts a BucketStore.
type BucketOption func(*BucketStore)

// WithStoreName labels what this store sends to the object store, so the
// cost of one store can be told from another's in a process holding
// several of them.
func WithStoreName(name string) BucketOption {
	return func(c *BucketStore) {
		c.attrs = append(c.attrs, keyStore.String(name))
	}
}

func (c *BucketStore) openFile(key string) (pack.File, error) {
	// a file still uploading is not in the bucket yet
	c.openFilesMtx.Lock()
	file, ok := c.openFiles[key]
	c.openFilesMtx.Unlock()

	if ok {
		return file, nil
	}

	attrs, err := c.bucket.Attributes(context.Background(), key)
	c.count(OpHead, 0)

	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, pack.ErrFileNotFound
		}
		return nil, err
	}

	return &bucketFile{
		key:      key,
		attrs:    attrs,
		store:    c,
		readOnly: true,
	}, nil
}

func (c *BucketStore) newWriteFile(key string) (pack.File, error) {
	writer, err := c.bucket.NewWriter(context.Background(), key, nil)
	if err != nil {
		c.count(OpPut, 0)

		return nil, err
	}

	file := &bucketFile{
		writer: writer,
		store:  c,
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

// Open implements pack.ArchiveStorage.
func (c *BucketStore) Open(name string) (pack.File, error) {
	return c.openFile(c.key(name))
}

// Create implements pack.ArchiveStorage.
func (c *BucketStore) Create(name string) (pack.File, error) {
	return c.newWriteFile(c.key(name))
}

// CreateNew implements pack.ArchiveStorage with the bucket's own
// precondition, so it holds across processes on GCS and S3; a file://
// bucket only checks within one process.
func (c *BucketStore) CreateNew(name string, data []byte) error {
	err := c.bucket.WriteAll(context.Background(), c.key(name), data, &blob.WriterOptions{IfNotExist: true})
	c.count(OpPut, int64(len(data)))

	if gcerrors.Code(err) == gcerrors.FailedPrecondition {
		return pack.ErrFileExists
	}

	return err
}

// Checksum implements pack.Checksummer with the MD5 the bucket reports,
// empty when it has none.
func (c *BucketStore) Checksum(name string) ([]byte, error) {
	attrs, err := c.bucket.Attributes(context.Background(), c.key(name))
	c.count(OpHead, 0)

	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, pack.ErrFileNotFound
		}

		return nil, err
	}

	return attrs.MD5, nil
}

// Delete implements pack.ArchiveStorage.
func (c *BucketStore) Delete(name string) error {
	c.count(OpDelete, 0)

	err := c.bucket.Delete(context.Background(), c.key(name))
	if gcerrors.Code(err) == gcerrors.NotFound {
		return pack.ErrFileNotFound
	}

	return err
}

// listPageSize is how many keys one listing request asks for, which is
// the most either S3 or Cloud Storage answers with.
const listPageSize = 1000

// pages walks a prefix a page at a time, because a listing is billed by
// the page rather than by the walk.
func (c *BucketStore) pages(prefix string, each func(*blob.ListObject) error) error {
	options := &blob.ListOptions{Prefix: prefix}

	for token := blob.FirstPageToken; ; {
		page, next, err := c.bucket.ListPage(context.Background(), token, listPageSize, options)
		c.count(OpList, 0)

		if err != nil {
			return err
		}

		for _, object := range page {
			if object.IsDir {
				continue
			}

			err = each(object)
			if err != nil {
				return err
			}
		}

		if len(next) == 0 {
			return nil
		}

		token = next
	}
}

// DeleteAll implements pack.ArchiveStorage.
func (c *BucketStore) DeleteAll() error {
	return c.pages(blobObjectPrefix, func(object *blob.ListObject) error {
		c.count(OpDelete, 0)

		return c.bucket.Delete(context.Background(), object.Key)
	})
}

// List implements pack.ArchiveStorage.
func (c *BucketStore) List(extension string) ([]string, error) {
	prefix := blobObjectPrefix
	if extension != "" {
		if len(extension) < 2 || extension[0] != '.' {
			return nil, pack.ErrInvalidExtension
		}

		prefix = fmt.Sprintf(blobObjectKey, extension, "")
	}

	var names []string

	err := c.pages(prefix, func(object *blob.ListObject) error {
		// keys are pack/<extension>/<name>; the name may hold slashes
		name := strings.TrimPrefix(object.Key, blobObjectPrefix)
		if _, rest, ok := strings.Cut(name, "/"); ok && extension == "" {
			name = rest
		} else if extension != "" {
			name = strings.TrimPrefix(object.Key, prefix)
		}

		names = append(names, name)

		return nil
	})

	return names, err
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

// NewBucketStore returns an archive storage over bucket.
func NewBucketStore(bucket *blob.Bucket, options ...BucketOption) *BucketStore {
	storage := &BucketStore{
		bucket:    bucket,
		openFiles: make(map[string]*bucketFile),
	}

	for _, option := range options {
		option(storage)
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
		pack.WithCompaction(pack.CompactionConfig{MinimumCandidates: 1000}),
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
