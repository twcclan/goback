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
	dir := tempDir(tb)

	tb.Logf("Creating badger index in %s", dir)
	idx, err := NewBadgerIndex(dir)
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

func setupBadger(t *testing.T, n int) (*BadgerIndex, []testArchive) {
	t.Helper()

	idx := tempIndex(t)
	t.Cleanup(func() {
		err := idx.Close()
		if err != nil {
			t.Logf("Failed closing badger index: %s", err)
		}
	})

	archives := make([]testArchive, n)
	for i := range archives {
		log.Printf("Creating test archive %d/%d", i+1, n)
		archives[i] = testArchive{uuid.New().String(), makeIndex()}
	}

	return idx, archives
}

func TestBadgerIndex(t *testing.T) {
	idx, archives := setupBadger(t, 10)

	for i, archive := range archives {
		log.Printf("indexing test archive %d/%d", i+1, len(archives))
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

	for _, archive := range archives {
		err := idx.Delete(archive.name, archive.index)
		if err != nil {
			t.Errorf("Couldn't delete archive from index: %s", err)
			continue
		}

		for _, record := range archive.index {
			_, err := idx.Locate(&proto.Ref{Sha1: record.Sum[:]})
			if err != ErrRecordNotFound {
				t.Errorf("Expected to not find index record for key %x: %s", record.Sum, err)
			}
		}
	}
}

func TestBadgerIndexExclusion(t *testing.T) {
	idx, archives := setupBadger(t, 10)

	// replace the second half of the archives with a copy of the first
	for i := 0; i < len(archives)/2; i++ {
		to := i + len(archives)/2
		from := i

		archives[to].index = archives[from].index
	}

	for _, archive := range archives {
		err := idx.Index(archive.name, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for _, archive := range archives[:len(archives)/2] {
		for _, i := range rand.Perm(len(archive.index)) {
			record := archive.index[i].Sum[:]

			location1, err := idx.Locate(&proto.Ref{Sha1: record})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			location2, err := idx.Locate(&proto.Ref{Sha1: record}, archive.name)
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			if cmp.Equal(location1.Archive, location2.Archive) {
				t.Errorf("Expected to find two different archives for record: %x", record)
			}
		}
	}
}
