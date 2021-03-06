package pack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type IndexFile []IndexRecord

var indexEndianness = binary.BigEndian

// increment when you make backwards-incompatible changes
var indexFileMagicBytes = []byte("GOBACKIDX_0001")
var errIndexHeaderMismatch = errors.New("received unexpected index file header")

func (idx IndexFile) Len() int           { return len(idx) }
func (idx IndexFile) Swap(i, j int)      { idx[i], idx[j] = idx[j], idx[i] }
func (idx IndexFile) Less(i, j int) bool { return bytes.Compare(idx[i].Sum[:], idx[j].Sum[:]) < 0 }

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

type IndexRecord struct {
	Sum    [20]byte
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
