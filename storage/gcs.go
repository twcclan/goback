package storage

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"cloud.google.com/go/storage"

	"github.com/Sirupsen/logrus"
	"github.com/twcclan/goback/backup"

	"github.com/pkg/errors"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
}

const gcsObjectKey = "pack/%s/%s" // pack/<extension>/<filename>
var gcsLogger = logrus.WithField("storage", "gcs")

var _ io.ReadSeeker = (*gcsReader)(nil)
var _ io.WriterTo = (*gcsReader)(nil)

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
	s.logger.Info("Streaming whole file")
	reader, err := s.gcs.Bucket(s.attrs.Bucket).Object(s.attrs.Name).NewReader(context.Background())

	if err != nil {
		return 0, err
	}

	defer reader.Close()

	return io.Copy(w, reader)
}

func (s *gcsReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case os.SEEK_SET:
		s.offset = offset
	case os.SEEK_CUR:
		s.offset += offset
	case os.SEEK_END:
		s.offset = s.attrs.Size - offset
	default:
		return 0, errors.New("invalid whence value")
	}

	return s.offset, nil
}

type gcsFile struct {
	gcsWriter  *storage.Writer
	fileWriter *os.File
	writer     io.Writer

	fileReader *os.File
	gcsReader  *gcsReader

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

	s.fileReader.Close()
	s.fileWriter.Close()
	os.Remove(s.fileWriter.Name()) // it's just a temp file, don't care about error

	// wait for the upload to finish
	return err
}

func (s *gcsFile) Read(buf []byte) (int, error) {
	if s.readOnly {
		return s.gcsReader.Read(buf)
	}

	return s.fileReader.Read(buf)
}

func (s *gcsFile) Write(buf []byte) (int, error) {
	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.writer.Write(buf)
}

func (s *gcsFile) Seek(offset int64, whence int) (int64, error) {
	if s.readOnly {
		return s.gcsReader.Seek(offset, whence)
	}

	return s.fileReader.Seek(offset, whence)
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

var _ File = (*gcsFile)(nil)

type gcsStore struct {
	bucket    string
	gcs       *storage.Client
	openFiles map[string]*gcsFile
}

func (s *gcsStore) openGCSFile(key string) (File, error) {
	gcsLogger.WithField("key", key).Debug("Opening file")
	// if this is a file we are currently uploading
	// return the active instance instead
	if file, ok := s.openFiles[key]; ok {
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

func (s *gcsStore) newGCSFile(key string) (File, error) {
	gcsLogger.WithField("key", key).Debug("Creating file")
	fileWriter, err := ioutil.TempFile("", "goback-gcs")
	if err != nil {
		return nil, err
	}

	fileReader, err := os.Open(fileWriter.Name())
	if err != nil {
		return nil, err
	}

	writer := s.gcs.Bucket(s.bucket).Object(key).NewWriter(context.Background())

	file := &gcsFile{
		gcsWriter:  writer,
		fileWriter: fileWriter,
		fileReader: fileReader,
		writer:     io.MultiWriter(fileWriter, writer),
		gcsReader: &gcsReader{
			gcs: s.gcs,
		},
	}

	s.openFiles[key] = file

	return file, nil
}

func (s *gcsStore) key(name string) string {
	return fmt.Sprintf(gcsObjectKey, path.Ext(name), name)
}

func (s *gcsStore) Open(name string) (File, error) {
	return s.openGCSFile(s.key(name))
}

func (s *gcsStore) Create(name string) (File, error) {
	return s.newGCSFile(s.key(name))
}

func (s *gcsStore) Delete(name string) error {
	return s.gcs.Bucket(s.bucket).Object(s.key(name)).Delete(context.Background())
}

func (s *gcsStore) List() ([]string, error) {
	iter := s.gcs.Bucket(s.bucket).Objects(context.Background(), &storage.Query{
		Prefix:    fmt.Sprintf(gcsObjectKey, ArchiveSuffix, ""),
		Delimiter: "/",
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

		gcsLogger.WithField("key", attrs.Name).Debug("Scanning object")

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

var _ ArchiveStorage = (*gcsStore)(nil)

func NewGCSObjectStore(serviceAccountFile string, bucket string) (backup.ObjectStore, error) {
	client, err := storage.NewClient(context.Background(), option.WithServiceAccountFile(serviceAccountFile))
	if err != nil {
		return nil, err
	}

	storage := &gcsStore{
		bucket:    bucket,
		gcs:       client,
		openFiles: make(map[string]*gcsFile),
	}

	return NewPackStorage(storage), nil
}
