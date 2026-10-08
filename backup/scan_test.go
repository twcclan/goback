package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAReadAheadListsDirectoriesBeforeTheWalkReachesThem(t *testing.T) {
	f := newWalkerFixture(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		f.write(name+"/file.txt", []byte(name))
	}

	s := newScanner(context.Background(), f.walker, 4, 0)
	defer s.close()

	root := s.take(scanJob{dir: f.root})
	require.NoError(t, root.err)
	require.Len(t, root.entries, 4)

	require.Eventually(t, func() bool {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		return len(s.ready) == 4
	}, 5*time.Second, time.Millisecond, "the read-ahead lists what the walk has not asked for yet")

	child := s.take(scanJob{dir: filepath.Join(f.root, "a"), rel: "a"})
	require.NoError(t, child.err)
	require.Len(t, child.entries, 1)
	require.Equal(t, "file.txt", child.entries[0].name)

	s.mtx.Lock()
	defer s.mtx.Unlock()

	require.Len(t, s.ready, 3, "the walk took the listing instead of making it")
	require.Equal(t, 3, s.held)
}

func TestAReadAheadHoldsNoMoreThanItsWindow(t *testing.T) {
	f := newWalkerFixture(t)
	for dir := range 20 {
		for file := range 5 {
			f.write(fmt.Sprintf("d%02d/f%d.txt", dir, file), []byte("x"))
		}
	}

	const window = 8

	s := newScanner(context.Background(), f.walker, 4, window)
	defer s.close()

	root := s.take(scanJob{dir: f.root})
	require.NoError(t, root.err)
	require.Len(t, root.entries, 20)

	held := func() int {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		return s.held
	}

	require.Eventually(t, func() bool { return held() > 0 }, 5*time.Second, time.Millisecond)
	require.Never(t, func() bool { return held() > window }, 200*time.Millisecond, 5*time.Millisecond)
}

func TestAReadAheadLeavesOutWhatTheWalkExcludes(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("keep/a.txt", []byte("a"))
	f.write("skip/b.txt", []byte("b"))
	f.walker.Include = func(rel string) bool { return rel != "skip" }

	s := newScanner(context.Background(), f.walker, 2, 0)
	defer s.close()

	root := s.take(scanJob{dir: f.root})
	require.NoError(t, root.err)
	require.Len(t, root.entries, 2)

	for _, entry := range root.entries {
		if entry.name == "skip" {
			require.Nil(t, entry.info, "an excluded entry costs no stat")
		}
	}

	require.Never(t, func() bool {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		_, ok := s.ready[filepath.Join(f.root, "skip")]

		return ok
	}, 200*time.Millisecond, 5*time.Millisecond, "an excluded directory is never listed")
}

func TestAWalkReadsAheadOfItself(t *testing.T) {
	f := newWalkerFixture(t)
	for dir := range 8 {
		for file := range 4 {
			f.write(fmt.Sprintf("d%d/f%d.bin", dir, file), f.random(64<<10))
		}
	}

	var ahead atomic.Bool

	// directories walked one at a time leave the read-ahead room to show
	f.walker.dirSlots = make(chan struct{})
	f.walker.ProgressInterval = time.Millisecond
	f.walker.Progress = func(WalkResult) {
		f.walker.scan.mtx.Lock()
		defer f.walker.scan.mtx.Unlock()

		if len(f.walker.scan.ready) > 0 {
			ahead.Store(true)
		}
	}

	f.run()

	require.True(t, ahead.Load(), "a run lists directories before the walk reaches them")
}

func TestAWalkCommitsTheSameTreeWithAndWithoutReadAhead(t *testing.T) {
	ahead := newWalkerFixture(t)
	ahead.write("a.txt", []byte("hello"))
	ahead.write("sub/b.bin", ahead.random(150<<10))
	ahead.write("sub/deep/c.txt", []byte("deep"))
	ahead.write("sub/deep/more/d.txt", []byte("more"))
	ahead.write("skip/e.txt", []byte("excluded"))

	include := func(rel string) bool { return !strings.HasPrefix(rel, "skip") }

	// a window of one entry leaves every listing waiting for the walk to
	// take the one before it, which is where a read-ahead would deadlock
	ahead.walker.Include = include
	ahead.walker.ScanWindow = 1

	behind := newWalkerFixture(t)
	behind.root = ahead.root
	behind.walker.Root = ahead.root
	behind.walker.Include = include
	behind.walker.ScanWorkers = -1

	require.True(t, ahead.run().Commit.Tree.Equal(behind.run().Commit.Tree))
}

func TestWalkingDirectoriesAtOnceCommitsTheTreeOfASerialWalk(t *testing.T) {
	parallel := newWalkerFixture(t)
	for a := range 4 {
		for b := range 4 {
			for c := range 3 {
				parallel.write(fmt.Sprintf("d%d/e%d/f%d/file.txt", a, b, c), []byte(fmt.Sprint(a, b, c)))
			}

			parallel.write(fmt.Sprintf("d%d/e%d/side.bin", a, b), parallel.random(8<<10))
		}
	}

	// a window of one entry keeps several walks waiting on listings at once
	parallel.walker.ScanWindow = 1

	serial := newWalkerFixture(t)
	serial.root = parallel.root
	serial.walker.Root = parallel.root
	serial.walker.dirSlots = make(chan struct{})

	require.True(t, parallel.run().Commit.Tree.Equal(serial.run().Commit.Tree))
}
