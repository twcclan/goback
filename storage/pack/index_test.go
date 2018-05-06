package pack

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndex(t *testing.T) {
	buf := new(bytes.Buffer)
	idx := make(index, 1000)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		idx[i].Length = rand.Uint32()
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
