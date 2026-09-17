package pack_test

import (
	"testing"

	"github.com/twcclan/goback/storage/pack"
	"github.com/twcclan/goback/storage/pack/packtest"
)

func TestInMemoryIndexSessions(t *testing.T) {
	packtest.TestArchiveIndexSessions(t, pack.NewInMemoryIndex())
}
