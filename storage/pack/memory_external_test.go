package pack_test

import (
	"testing"

	"github.com/gobackio/goback/storage/pack"
	"github.com/gobackio/goback/storage/pack/packtest"
)

func TestInMemoryIndexSessions(t *testing.T) {
	packtest.TestArchiveIndexSessions(t, pack.NewInMemoryIndex())
}

func TestInMemoryArchiveVersions(t *testing.T) {
	packtest.TestArchiveVersions(t, pack.NewInMemoryIndex())
}

func TestInMemoryClaimIndex(t *testing.T) {
	packtest.TestClaimIndex(t, pack.NewInMemoryIndex())
}
