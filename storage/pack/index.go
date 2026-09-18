package pack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sort"

	"github.com/twcclan/goback/proto"
)

// IndexFile is an archive's index; a stored one is sorted by Sum.
type IndexFile []IndexRecord

var indexEndianness = binary.BigEndian

// increment when you make backwards-incompatible changes
var indexFileMagicBytes = []byte("GOBACKIDX_0002")
var errIndexHeaderMismatch = errors.New("received unexpected index file header")

// Len implements sort.Interface.
func (idx IndexFile) Len() int           { return len(idx) }
// Swap implements sort.Interface.
func (idx IndexFile) Swap(i, j int)      { idx[i], idx[j] = idx[j], idx[i] }
// Less implements sort.Interface.
func (idx IndexFile) Less(i, j int) bool { return bytes.Compare(idx[i].Sum[:], idx[j].Sum[:]) < 0 }

// position returns the index of the record for hash, or -1.
func (idx IndexFile) position(hash []byte) int {
	i := sort.Search(len(idx), func(i int) bool { return bytes.Compare(idx[i].Sum[:], hash) >= 0 })
	if i < len(idx) && bytes.Equal(idx[i].Sum[:], hash) {
		return i
	}

	return -1
}

// indexScanner yields an archive index's records in stored order, which is
// by Sum. A scanner is used once and closed.
type indexScanner interface {
	// next returns the next record, or nil at the end.
	next() (*IndexRecord, error)
	close() error
}

// fileScanner streams an index straight off its file, so a caller that
// only walks it never holds it.
type fileScanner struct {
	file File
	buf  *bufio.Reader
	left uint32
}

func newFileScanner(file File) (*fileScanner, error) {
	buf := bufio.NewReader(file)

	magic := make([]byte, len(indexFileMagicBytes))
	if _, err := io.ReadFull(buf, magic); err != nil {
		return nil, err
	}

	if !bytes.Equal(magic, indexFileMagicBytes) {
		return nil, errIndexHeaderMismatch
	}

	var count uint32
	if err := binary.Read(buf, indexEndianness, &count); err != nil {
		return nil, err
	}

	return &fileScanner{file: file, buf: buf, left: count}, nil
}

func (s *fileScanner) next() (*IndexRecord, error) {
	if s.left == 0 {
		return nil, nil
	}

	record := new(IndexRecord)
	if err := binary.Read(s.buf, indexEndianness, record); err != nil {
		return nil, err
	}

	s.left--

	return record, nil
}

func (s *fileScanner) close() error { return s.file.Close() }

// sliceScanner serves an index that is already in memory, which is what an
// archive whose index had to be recovered has.
type sliceScanner struct {
	idx IndexFile
	pos int
}

func (s *sliceScanner) next() (*IndexRecord, error) {
	if s.pos >= len(s.idx) {
		return nil, nil
	}

	record := &s.idx[s.pos]
	s.pos++

	return record, nil
}

func (s *sliceScanner) close() error { return nil }

// ReadFrom implements io.ReaderFrom.
func (idx *IndexFile) ReadFrom(reader io.Reader) (int64, error) {
	buf := bufio.NewReader(reader)
	var count uint32
	byteCounter := &countingWriter{}

	source := io.TeeReader(buf, byteCounter)

	magic := make([]byte, len(indexFileMagicBytes))
	_, err := io.ReadFull(source, magic)
	if err != nil {
		return byteCounter.count, err
	}

	if !bytes.Equal(magic, indexFileMagicBytes) {
		return byteCounter.count, errIndexHeaderMismatch
	}

	err = binary.Read(source, indexEndianness, &count)
	if err != nil {
		return 0, err
	}

	*idx = make([]IndexRecord, count)

	for i := 0; i < int(count); i++ {

		idxSlice := *idx
		err = binary.Read(source, indexEndianness, &idxSlice[i])
		if err != nil {
			return 0, err
		}
	}

	return byteCounter.count, nil
}

// WriteTo implements io.WriterTo.
func (idx IndexFile) WriteTo(writer io.Writer) (int64, error) {
	buf := bufio.NewWriter(writer)
	count := uint32(len(idx))
	byteCounter := &countingWriter{}

	target := io.MultiWriter(buf, byteCounter)

	n, err := target.Write(indexFileMagicBytes)
	if err != nil {
		return int64(n), err
	}

	err = binary.Write(target, indexEndianness, count)
	if err != nil {
		return 0, err
	}

	for _, record := range idx {
		err = binary.Write(target, indexEndianness, &record)
		if err != nil {
			return 0, err
		}
	}

	return byteCounter.count, buf.Flush()
}

// IndexRecord locates one object in its archive.
type IndexRecord struct {
	Sum    [proto.HashSize]byte
	Offset uint32
	Length uint32
	Type   uint32
}

var _ io.WriterTo = (IndexFile)(nil)
var _ io.ReaderFrom = (*IndexFile)(nil)

type countingWriter struct {
	count int64
}

func (c *countingWriter) Write(data []byte) (int, error) {
	c.count += int64(len(data))
	return len(data), nil
}
