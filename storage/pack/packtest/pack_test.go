package packtest

import (
	"testing"

	"github.com/twcclan/goback/storage/pack"
)

func TestInMemoryIndex(t *testing.T) {
	index := pack.NewInMemoryIndex()

	TestArchiveIndex(t, index)
}

func TestInMemoryIndexSessions(t *testing.T) {
	TestArchiveIndexSessions(t, pack.NewInMemoryIndex())
}
