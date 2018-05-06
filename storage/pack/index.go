package pack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"sort"

	"github.com/twcclan/goback/proto"
)

type index []indexRecord

var indexEndianness = binary.BigEndian

func (b index) Len() int           { return len(b) }
func (b index) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b index) Less(i, j int) bool { return bytes.Compare(b[i].Sum[:], b[j].Sum[:]) < 0 }

func (idx *index) ReadFrom(reader io.Reader) (int64, error) {
	buf := bufio.NewReader(reader)
	var count uint32
	byteCounter := &countingWriter{}

	source := io.TeeReader(buf, byteCounter)

	err := binary.Read(source, indexEndianness, &count)
	if err != nil {
		return 0, err
	}

	*idx = make([]indexRecord, count)

	for i := 0; i < int(count); i++ {

		idxSlice := *idx
		err = binary.Read(source, indexEndianness, &idxSlice[i])
		if err != nil {
			return 0, err
		}
	}

	return byteCounter.count, nil
}

func (idx index) lookup(ref *proto.Ref) *indexRecord {
	n := sort.Search(len(idx), func(i int) bool {
		return bytes.Compare(idx[i].Sum[:], ref.Sha1) >= 0
	})

	if n < len(idx) && bytes.Equal(idx[n].Sum[:], ref.Sha1) {
		return &idx[n]
	}

	return nil
}

func (idx index) WriteTo(writer io.Writer) (int64, error) {
	buf := bufio.NewWriter(writer)
	count := uint32(len(idx))
	byteCounter := &countingWriter{}

	target := io.MultiWriter(buf, byteCounter)

	err := binary.Write(target, indexEndianness, count)
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

type indexRecord struct {
	Sum    [20]byte
	Offset uint32
	Length uint32
	Pack   uint16
}

var _ io.WriterTo = (index)(nil)
var _ io.ReaderFrom = (*index)(nil)

type countingWriter struct {
	count int64
}

func (c *countingWriter) Write(data []byte) (int, error) {
	c.count += int64(len(data))
	return len(data), nil
}
