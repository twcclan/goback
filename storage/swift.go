package storage

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"

	"github.com/ncw/swift"
	"github.com/pkg/errors"
)

const objectKey = "pack/%s/%s" // pack/<extension>/<filename>

var _ io.ReadSeeker = (*swift.ObjectOpenFile)(nil)

type swiftFile struct {
	con       *swift.Connection
	container string
	key       string
	readOnly  bool

	fileWriter  *os.File
	swiftWriter *swift.ObjectCreateFile
	writer      io.Writer

	fileReader  *os.File
	swiftReader *swift.ObjectOpenFile
}

func (s *swiftFile) Close() error {
	if s.readOnly {
		// this is a nop for read-only files
		return s.swiftReader.Close()
	}

	s.readOnly = true

	s.fileReader.Close()
	s.fileWriter.Close()
	os.Remove(s.fileWriter.Name()) // it's just a temp file, don't care about error

	// wait for the upload to finish
	err := s.swiftWriter.Close()
	if err != nil {
		return err
	}

	s.swiftReader, _, err = s.con.ObjectOpen(s.container, s.key, false, swift.Headers{})

	return err
}

func (s *swiftFile) Read(buf []byte) (int, error) {
	if s.readOnly {
		return s.swiftReader.Read(buf)
	}

	return s.fileReader.Read(buf)
}

func (s *swiftFile) Write(buf []byte) (int, error) {
	if s.readOnly {
		return 0, errors.New("Cannot write to read only file")
	}

	return s.writer.Write(buf)
}

func (s *swiftFile) Seek(offset int64, whence int) (int64, error) {
	if s.readOnly {
		return s.swiftReader.Seek(offset, whence)
	}

	return s.fileReader.Seek(offset, whence)
}

func (s *swiftFile) Stat() (os.FileInfo, error) {
	info, _, err := s.con.Object(s.container, s.key)

	return &swiftFileInfo{info}, err
}

var _ File = (*swiftFile)(nil)

type swiftFileInfo struct {
	swift.Object
}

func (s *swiftFileInfo) Sys() interface{} {
	return nil
}

func (s *swiftFileInfo) Size() int64 {
	return s.Object.Bytes
}

func (s *swiftFileInfo) Name() string {
	return path.Base(s.Object.Name)
}

func (s *swiftFileInfo) Mode() os.FileMode {
	return 0
}

func (s *swiftFileInfo) ModTime() time.Time {
	return s.LastModified
}

func (s *swiftFileInfo) IsDir() bool {
	return false
}

var _ os.FileInfo = (*swiftFileInfo)(nil)

type swiftStorage struct {
	container string
	con       *swift.Connection
	openFiles map[string]*swiftFile
}

func (s *swiftStorage) openSwiftFile(key string) (File, error) {
	// if this is a file we are currently uploading
	// return the active instance instead
	if file, ok := s.openFiles[key]; ok {
		return file, nil
	}

	reader, _, err := s.con.ObjectOpen(s.container, key, false, nil)
	if err != nil {
		return nil, err
	}

	return &swiftFile{
		swiftReader: reader,
		readOnly:    true,
		con:         s.con,
		container:   s.container,
		key:         key,
	}, nil
}

func (s *swiftStorage) newSwiftFile(key string) (File, error) {
	fileWriter, err := ioutil.TempFile("", "goback-s3")
	if err != nil {
		return nil, err
	}

	fileReader, err := os.Open(fileWriter.Name())
	if err != nil {
		return nil, err
	}

	swiftWriter, err := s.con.ObjectCreate(s.container, key, true, "", "", nil)
	if err != nil {
		return nil, err
	}

	file := &swiftFile{
		swiftWriter: swiftWriter,
		fileWriter:  fileWriter,
		writer:      io.MultiWriter(swiftWriter, fileWriter),
		fileReader:  fileReader,
		container:   s.container,
		con:         s.con,
		key:         key,
	}

	s.openFiles[key] = file

	return file, nil
}

func (s *swiftStorage) key(name string) string {
	return fmt.Sprintf(objectKey, path.Ext(name), name)
}

func (s *swiftStorage) Open(name string) (File, error) {
	return s.openSwiftFile(s.key(name))
}

func (s *swiftStorage) Create(name string) (File, error) {
	return s.newSwiftFile(s.key(name))
}

func (s *swiftStorage) Delete(name string) error {
	return s.con.ObjectDelete(s.container, s.key(name))
}

func (s *swiftStorage) List() ([]string, error) {
	names, err := s.con.ObjectNamesAll(s.container, &swift.ObjectsOpts{
		Prefix: fmt.Sprintf(objectKey, ArchiveSuffix, ""),
	})

	if err != nil {
		return nil, err
	}

	for i, name := range names {
		// strip prefix and extension
		names[i] = strings.TrimSuffix(path.Base(name), path.Ext(name))
	}

	return names, nil
}

var _ ArchiveStorage = (*swiftStorage)(nil)

func NewSwiftObjectStore(username, apiKey, authURL, tenant, container string) backup.ObjectStore {
	// connection := &swift.Connection{
	// 	UserName: "e2K4x5cQ85Fu",
	// 	ApiKey:   "FDm9tDJsZa97Qez9rkTaeUBQkDdTS8w6",
	// 	AuthUrl:  "https://auth.cloud.ovh.net/v2.0",
	// 	Tenant:   "1675837378358194",
	// }

	connection := &swift.Connection{
		UserName: username,
		ApiKey:   apiKey,
		AuthUrl:  authURL,
		Tenant:   tenant,
		Region:   "SBG1",
	}

	err := connection.Authenticate()
	if err != nil {
		log.Fatal(err)
	}

	storage := &swiftStorage{
		container: container,
		con:       connection,
		openFiles: make(map[string]*swiftFile),
	}

	return NewPackStorage(storage)
}
