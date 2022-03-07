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

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func init() {
	logrus.SetLevel(logrus.InfoLevel)
}

const gcsObjectKey = "pack/%s/%s" // pack/<extension>/<filename>
var gcsLogger = logrus.WithField("storage", "gcs")

var _ io.ReadSeeker = (*gcsReader)(nil)
var _ io.WriterTo = (*gcsReader)(nil)
var _ io.ReaderAt = (*gcsFile)(nil)

type gcsReader struct {
	logger logrus.FieldLogger
	gcs    *storage.Client
	attrs  *storage.ObjectAttrs
	offset int64
}

func (s *gcsReader) Read(buf []byte) (int, error) {
	n, err := s.ReadAt(buf, s.offset)
	s.offset += int64(n)

	return n, err
}

func (s *gcsReader) ReadAt(buf []byte, offset int64) (int, error) {
	if offset >= s.attrs.Size {
		return 0, io.EOF
	}

	length := int64(len(buf))

	if offset+length >= s.attrs.Size {
		length = s.attrs.Size - offset
	}

	objHandle := s.gcs.Bucket(s.attrs.Bucket).Object(s.attrs.Name)
	reader, err := objHandle.NewRangeReader(context.Background(), offset, length)

	if err != nil {
		return 0, err
	}

	defer reader.Close()

	return io.ReadFull(reader, buf[:length])
}

func (s *gcsReader) WriteTo(w io.Writer) (int64, error) {
	reader, err := s.gcs.Bucket(s.attrs.Bucket).Object(s.attrs.Name).NewReader(context.Background())

	if err != nil {
		return 0, err
	}

	defer reader.Close()

	return io.Copy(w, reader)
}

func (s *gcsReader) Seek(offset int64, whence int) (int64, error) {
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

type gcsFile struct {
	gcsWriter *storage.Writer
	gcsReader *gcsReader

	readOnly bool
}

func (s *gcsFile) Close() error {
	if s.readOnly {
		// this is a nop for read-only files
		return nil
	}

	s.readOnly = true

	err := s.gcsWriter.Close()
	if err == nil {
		s.gcsReader.attrs = s.gcsWriter.Attrs()
	}

	// wait for the upload to finish
	return err
}

func (s *gcsFile) Read(buf []byte) (int, error) {
	if s.readOnly {
		return s.gcsReader.Read(buf)
	}

	return 0, errors.New("Read only supported for readonly files")
}

func (s *gcsFile) ReadAt(buf []byte, off int64) (int, error) {
	if s.readOnly {
		return s.gcsReader.ReadAt(buf, off)
	}

	return 0, errors.New("Read only supported for readonly files")
}

func (s *gcsFile) Write(buf []byte) (int, error) {
	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.gcsWriter.Write(buf)
}

func (s *gcsFile) Seek(offset int64, whence int) (int64, error) {
	if s.readOnly {
		return s.gcsReader.Seek(offset, whence)
	}

	return -1, errors.New("Seek only supported for readonly files")
}

func (s *gcsFile) WriteTo(w io.Writer) (int64, error) {
	if s.readOnly {
		return s.gcsReader.WriteTo(w)
	}

	return -1, errors.New("WriteTo only supported for readonly files")
}

func (s *gcsFile) Stat() (os.FileInfo, error) {
	return &gcsFileInfo{attrs: s.gcsReader.attrs}, nil
}

var _ pack.File = (*gcsFile)(nil)

type gcsStore struct {
	bucket string
	gcs    *storage.Client

	openFilesMtx sync.Mutex
	openFiles    map[string]*gcsFile
}

func (s *gcsStore) CloseBeforeRead() {}

func (s *gcsStore) openGCSFile(key string) (pack.File, error) {
	gcsLogger.WithField("key", key).Debug("Opening file")
	// if this is a file we are currently uploading
	// return the active instance instead
	s.openFilesMtx.Lock()
	file, ok := s.openFiles[key]
	s.openFilesMtx.Unlock()

	if ok {
		return file, nil
	}

	// request information about the file
	attrs, err := s.gcs.Bucket(s.bucket).Object(key).Attrs(context.Background())
	if err != nil {
		return nil, err
	}

	return &gcsFile{
		gcsReader: &gcsReader{
			logger: gcsLogger.WithField("key", key),
			attrs:  attrs,
			gcs:    s.gcs,
		},
		readOnly: true,
	}, nil
}

func (s *gcsStore) newGCSFile(key string) (pack.File, error) {
	writer := s.gcs.Bucket(s.bucket).Object(key).NewWriter(context.Background())

	file := &gcsFile{
		gcsWriter: writer,
		gcsReader: &gcsReader{
			gcs: s.gcs,
		},
	}

	s.openFilesMtx.Lock()
	s.openFiles[key] = file
	s.openFilesMtx.Unlock()

	return file, nil
}

func (s *gcsStore) key(name string) string {
	return fmt.Sprintf(gcsObjectKey, path.Ext(name), name)
}

func (s *gcsStore) Open(name string) (pack.File, error) {
	return s.openGCSFile(s.key(name))
}

func (s *gcsStore) Create(name string) (pack.File, error) {
	return s.newGCSFile(s.key(name))
}

func (s *gcsStore) Delete(name string) error {
	return s.gcs.Bucket(s.bucket).Object(s.key(name)).Delete(context.Background())
}

func (s *gcsStore) DeleteAll() error {
	return errors.New("not implemented")
}

func (s *gcsStore) List(extension string) ([]string, error) {
	prefix := blobObjectPrefix
	delimiter := ""
	if extension != "" {
		if len(extension) < 2 || extension[0] != '.' {
			return nil, pack.ErrInvalidExtension
		}

		prefix = fmt.Sprintf(blobObjectKey, extension, "")
		delimiter = "/"
	}

	iter := s.gcs.Bucket(s.bucket).Objects(context.Background(), &storage.Query{
		Prefix:    prefix,
		Delimiter: delimiter,
	})

	var names []string

	for {
		attrs, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}

			return names, err
		}

		name := strings.TrimSuffix(path.Base(attrs.Name), path.Ext(attrs.Name))
		names = append(names, name)
	}

	return names, nil
}

type gcsFileInfo struct {
	attrs *storage.ObjectAttrs
}

func (s *gcsFileInfo) Sys() interface{} {
	return nil
}

func (s *gcsFileInfo) Size() int64 {
	return s.attrs.Size
}

func (s *gcsFileInfo) Name() string {
	return path.Base(s.attrs.Name)
}

func (s *gcsFileInfo) Mode() os.FileMode {
	return 0
}

func (s *gcsFileInfo) ModTime() time.Time {
	return s.attrs.Updated
}

func (s *gcsFileInfo) IsDir() bool {
	return false
}

var _ os.FileInfo = (*gcsFileInfo)(nil)

var _ pack.ArchiveStorage = (*gcsStore)(nil)

func NewGCSObjectStore(bucket, indexDir, cacheDir string) (backup.ObjectStore, error) {
	credentials, err := google.FindDefaultCredentials(context.Background(), storage.ScopeReadWrite)
	if err != nil {
		return nil, err
	}
	client, err := storage.NewClient(context.Background(), option.WithCredentials(credentials))
	if err != nil {
		return nil, err
	}

	storage := &gcsStore{
		bucket:    bucket,
		gcs:       client,
		openFiles: make(map[string]*gcsFile),
	}

	err = os.MkdirAll(indexDir, 0644)
	if err != nil {
		return nil, err
	}

	idx, err := badgerIdx.NewBadgerIndex(indexDir)

	options := []pack.Option{
		pack.WithArchiveStorage(storage),
		pack.WithArchiveIndex(idx),
		pack.WithMaxParallel(64),
		pack.WithCloseBeforeRead(true),
		pack.WithMaxSize(1024 * 1024 * 1024),
		pack.WithCompaction(pack.CompactionConfig{
			Periodically:      24 * time.Hour,
			MinimumCandidates: 1000,
			GarbageCollection: false,
			OnOpen:            false,
		}),
	}

	if cacheDir != "" {
		err = os.MkdirAll(cacheDir, 0644)
		if err != nil {
			return nil, err
		}

		cache, err := badger.New(cacheDir)
		if err != nil {
			return nil, err
		}

		options = append(options, pack.WithMetadataCache(cache))
	}

	return pack.New(options...)
}
