package pack

import (
	"bytes"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func makeIndex() IndexFile {
	idx := make(IndexFile, 1000)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		idx[i].Length = rand.Uint32()
		idx[i].Type = rand.Uint32()
		for j := range idx[i].Sum {
			idx[i].Sum[j] = byte(rand.Intn(256))
		}
	}

	return idx
}

func TestIndex(t *testing.T) {
	buf := new(bytes.Buffer)
	idx := makeIndex()

	_, err := idx.WriteTo(buf)
	assert.Nil(t, err)

	var newIdx IndexFile

	_, err = (&newIdx).ReadFrom(bytes.NewReader(buf.Bytes()))
	assert.Nil(t, err)

	assert.Equal(t, idx, newIdx)
}

func TestIndexSize(t *testing.T) {
	rec := IndexRecord{}

	t.Logf("index record size: %d", unsafe.Sizeof(rec))
}
