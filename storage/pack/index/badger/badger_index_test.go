package badger

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/twcclan/goback/storage/pack/packtest"
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

func setupBadger(tb testing.TB) *BadgerIndex {
	tb.Helper()

	idx := tempIndex(tb)
	tb.Cleanup(func() {
		err := idx.Close()
		if err != nil {
			tb.Logf("Failed closing badger index: %s", err)
		}
	})

	return idx
}

func TestBadgerIndex(t *testing.T) {
	idx := setupBadger(t)

	packtest.TestArchiveIndex(t, idx)
}

func TestBadgerIndexExclusion(t *testing.T) {
	idx := setupBadger(t)

	packtest.TestBadgerIndexExclusion(t, idx)
}

func BenchmarkLookup(b *testing.B) {
	idx := setupBadger(b)

	b.Run("lookup", func(b *testing.B) {
		packtest.BenchmarkLookup(b, idx)
	})
}
