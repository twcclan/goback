package pack

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"
	"unsafe"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/assert"
)

func TestIndex(t *testing.T) {
	buf := new(bytes.Buffer)
	idx := make(index, 1000)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		idx[i].Length = rand.Uint32()
		idx[i].Type = rand.Uint32()
		for j := range idx[i].Sum {
			idx[i].Sum[j] = byte(rand.Intn(256))
		}
	}
	_, err := idx.WriteTo(buf)
	assert.Nil(t, err)

	var newIdx index

	_, err = (&newIdx).ReadFrom(bytes.NewReader(buf.Bytes()))
	assert.Nil(t, err)

	assert.Equal(t, idx, newIdx)
}

func TestIndexSize(t *testing.T) {
	rec := indexRecord{}

	t.Logf("index record size: %d", unsafe.Sizeof(rec))
}

func BenchmarkIndex(b *testing.B) {
	idx := make(index, b.N)
	refs := make([]proto.Ref, b.N)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		idx[i].Length = rand.Uint32()
		idx[i].Type = rand.Uint32()
		for j := range idx[i].Sum {
			idx[i].Sum[j] = byte(rand.Intn(256))
		}

		refs[i] = proto.Ref{Sha1: idx[i].Sum[:]}
	}

	sort.Sort(idx)
	order := rand.Perm(len(idx))

	b.ResetTimer()
	b.ReportAllocs()

	for _, i := range order {
		idx.lookup(&refs[i])
	}
}
