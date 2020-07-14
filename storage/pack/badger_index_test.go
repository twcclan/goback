package pack

import (
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

func tempDir(tb testing.TB) string {
	dir, err := ioutil.TempDir("", tb.Name())
	if err != nil {
		tb.Fatalf("failed creating temporary directory for test: %s", err.Error())
	}

	tb.Logf("creating temporary directory: %s", dir)

	tb.Cleanup(func() {
		tb.Logf("removing temporary directory: %s", dir)
		err := os.RemoveAll(dir)
		if err != nil {
			tb.Logf("failed removing temporary directory: %s", err.Error())
		}
	})

	return dir
}

func tempIndex(tb testing.TB) *BadgerIndex {
	idx, err := NewBadgerIndex("./badger")
	if err != nil {
		tb.Fatalf("failed creating index: %s", err)
	}

	tb.Cleanup(func() {
		tb.Log("closing badger index")

		err := idx.Close()
		if err != nil {
			tb.Logf("failed closing index: %s", err)
		}
	})

	return idx
}

type testArchive struct {
	name  string
	index IndexFile
}

func TestBadgerIndex(t *testing.T) {
	idx := tempIndex(t)

	archives := make([]testArchive, 100)
	for i := range archives {
		log.Printf("Creating test archive %d/%d", i+1, 100)
		archives[i] = testArchive{uuid.New().String(), makeIndex()}
	}

	for i, archive := range archives {
		log.Printf("indexing test archive %d/%d", i+1, 100)
		err := idx.Index(archive.name, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for i, archive := range archives {
		log.Printf("searching test archive %d/%d", i+1, 100)
		for _, i := range rand.Perm(len(archive.index)) {
			location, err := idx.Locate(&proto.Ref{Sha1: archive.index[i].Sum[:]})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			if !cmp.Equal(location.Archive, archive.name) {
				t.Errorf("archive name mismatch: %s != %s", location.Archive, archive.name)
			}

			if !cmp.Equal(location.Record, archive.index[i]) {
				t.Errorf(cmp.Diff(location.Record, archive.index[i]))
			}
		}
	}

}
