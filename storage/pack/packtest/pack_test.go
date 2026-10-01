package packtest

import (
	"testing"

	"github.com/twcclan/goback/storage/pack"
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
