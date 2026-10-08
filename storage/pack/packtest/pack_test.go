package packtest

import (
	"testing"

	"github.com/gobackio/goback/storage/pack"
)

func TestInMemoryIndex(t *testing.T) {
	index := pack.NewInMemoryIndex()

	TestArchiveIndex(t, index)
}

func TestInMemoryIndexCopies(t *testing.T) {
	TestArchiveIndexCopies(t, pack.NewInMemoryIndex())
}

func TestInMemoryIndexSessions(t *testing.T) {
	TestArchiveIndexSessions(t, pack.NewInMemoryIndex())
}

func TestInMemoryIndexTombstones(t *testing.T) {
	TestArchiveIndexTombstones(t, pack.NewInMemoryIndex())
}
