package storage

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/twcclan/goback/backup"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
)

const s3Key = "pack/%s/%s" // pack/<extension>/<filename>

var _ io.ReadSeeker = (*s3Reader)(nil)
var _ io.WriterTo = (*s3Reader)(nil)

type s3Reader struct {
	s3     *s3.S3
	bucket string
	key    string
	offset int64
	size   int64
}

func (s *s3Reader) Read(buf []byte) (int, error) {
	if s.offset >= s.size {
		return 0, io.EOF
	}

	limit := s.offset + int64(len(buf))
	if limit > s.size {
		limit = s.size
	}

	params := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", s.offset, limit-1)),
	}

	obj, err := s.s3.GetObject(params)

	if err != nil {
		return 0, err
	}

	defer obj.Body.Close()

	n, err := io.ReadFull(obj.Body, buf[:*obj.ContentLength])
	s.offset += int64(n)

	return n, err
}

func (s *s3Reader) WriteTo(w io.Writer) (int64, error) {
	params := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-", s.offset)),
	}

	obj, err := s.s3.GetObject(params)
	if err != nil {
		return 0, err
	}

	defer obj.Body.Close()

	return io.Copy(w, obj.Body)
}

func (s *s3Reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case os.SEEK_SET:
		s.offset = offset
	case os.SEEK_CUR:
		s.offset += offset
	case os.SEEK_END:
		s.offset = s.size - offset
	default:
		return 0, errors.New("invalid whence value")
	}

	return s.offset, nil
}

type s3File struct {
	pipeWriter io.WriteCloser
	fileWriter *os.File
	writer     io.Writer

	fileReader *os.File
	s3Reader   *s3Reader

	size     int64
	readOnly bool
	err      chan error
}

func (s *s3File) Close() error {
	if s.readOnly {
		// this is a nop for read-only files
		return nil
	}

	s.readOnly = true
	s.s3Reader.size = s.size

	s.pipeWriter.Close() // will never return an error
	s.fileReader.Close()
	s.fileWriter.Close()
	os.Remove(s.fileWriter.Name()) // it's just a temp file, don't care about error

	// wait for the upload to finish
	return <-s.err
}

func (s *s3File) Read(buf []byte) (int, error) {
	if s.readOnly {
		return s.s3Reader.Read(buf)
	}

	return s.fileReader.Read(buf)
}

func (s *s3File) Write(buf []byte) (int, error) {
	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.writer.Write(buf)
}

func (s *s3File) Seek(offset int64, whence int) (int64, error) {
	if s.readOnly {
		return s.s3Reader.Seek(offset, whence)
	}

	return s.fileReader.Seek(offset, whence)
}

var _ File = (*s3File)(nil)

type s3Storage struct {
	uploader  *s3manager.Uploader
	bucket    string
	s3        *s3.S3
	openFiles map[string]*s3File
}

func (s *s3Storage) openS3File(key string) (File, error) {
	// if this is a file we are currently uploading
	// return the active instance instead
	if file, ok := s.openFiles[key]; ok {
		return file, nil
	}

	// request information about the file
	// we are especially interested in the file size
	params := &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	info, err := s.s3.HeadObject(params)
	if err != nil {
		return nil, err
	}

	size := *info.ContentLength

	return &s3File{
		s3Reader: &s3Reader{
			size:   size,
			key:    key,
			bucket: s.bucket,
			s3:     s.s3,
		},
		readOnly: true,
	}, nil
}

func (s *s3Storage) newS3File(key string) (File, error) {
	fileWriter, err := ioutil.TempFile("", "goback-s3")
	if err != nil {
		return nil, err
	}

	fileReader, err := os.Open(fileWriter.Name())
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()

	params := &s3manager.UploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   reader,
	}

	errChan := make(chan error)

	// start the uploader goroutine
	go func() {
		_, err := s.uploader.Upload(params)
		if err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	file := &s3File{
		pipeWriter: writer,
		fileWriter: fileWriter,
		fileReader: fileReader,
		writer:     io.MultiWriter(fileWriter, writer),
		err:        errChan,
		s3Reader: &s3Reader{
			key:    key,
			bucket: s.bucket,
			s3:     s.s3,
		},
	}

	s.openFiles[key] = file

	return file, nil
}

func (s *s3Storage) key(name string) string {
	return fmt.Sprintf(s3Key, path.Ext(name), name)
}

func (s *s3Storage) Open(name string) (File, error) {
	return s.openS3File(s.key(name))
}

func (s *s3Storage) Create(name string) (File, error) {
	return s.newS3File(s.key(name))
}

func (s *s3Storage) List() ([]string, error) {
	params := &s3.ListObjectsInput{
		Prefix:    aws.String(fmt.Sprintf(s3Key, ArchiveSuffix, "")),
		Bucket:    aws.String(s.bucket),
		Delimiter: aws.String("/"),
	}
	var names []string

	for {
		list, err := s.s3.ListObjects(params)
		if err != nil {
			return nil, err
		}

		for _, object := range list.Contents {
			// strip prefix and extension
			name := strings.TrimSuffix(path.Base(*object.Key), path.Ext(*object.Key))
			names = append(names, name)
		}

		if !*list.IsTruncated {
			break
		}

		params.Marker = list.NextMarker
	}

	return names, nil
}

var _ ArchiveStorage = (*s3Storage)(nil)

func NewS3ChunkStore(region string, bucket string) backup.ObjectStore {
	session := session.New(aws.NewConfig().WithRegion(region).WithDisableSSL(true))

	storage := &s3Storage{
		uploader: s3manager.NewUploader(session, func(u *s3manager.Uploader) {
			u.LeavePartsOnError = false
			u.PartSize = 10 * 1024 * 1024
		}),
		bucket:    bucket,
		s3:        s3.New(session),
		openFiles: make(map[string]*s3File),
	}

	return NewPackStorage(storage)
}
