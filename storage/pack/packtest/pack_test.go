package packtest

import (
	"testing"

	"github.com/twcclan/goback/storage/pack"
)

func TestLocalArchiveStorage(t *testing.T) {
	store := pack.NewLocalArchiveStorage(t.TempDir())

	TestArchiveStorage(t, store)
}

func TestInMemoryIndex(t *testing.T) {
	index := pack.NewInMemoryIndex()

	TestArchiveIndex(t, index)
}