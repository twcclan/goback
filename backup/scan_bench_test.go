package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestReadAheadBenchmark measures what listing directories ahead of the
// walk is worth on a tree whose cost is metadata rather than content. It
// runs only with GOBACK_BENCH set.
func TestReadAheadBenchmark(t *testing.T) {
	if os.Getenv("GOBACK_BENCH") == "" {
		t.Skip("set GOBACK_BENCH to run")
	}

	var (
		dirs    = benchInt("GOBACK_BENCH_DIRS", 2_000)
		perDir  = benchInt("GOBACK_BENCH_FILES", 10)
		rounds  = benchInt("GOBACK_BENCH_ROUNDS", 3)
		workers = []int{-1, 1, 2, 4, 8, 16}
	)

	root := t.TempDir()
	for dir := range dirs {
		sub := filepath.Join(root, fmt.Sprintf("d%04d", dir))
		require.NoError(t, os.MkdirAll(sub, 0o755))

		for file := range perDir {
			require.NoError(t, os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%d.txt", file)), []byte("content"), 0o644))
		}
	}

	t.Logf("%d files in %d directories, best of %d", dirs*perDir, dirs, rounds)

	// the walks run in one order, so the filesystem must already hold the
	// tree's metadata before the first of them; otherwise every reading is
	// really measuring how much the one before it warmed up
	walkOnce(t, root, 8)

	best := make(map[int]time.Duration, len(workers))
	for range rounds {
		for _, count := range workers {
			took := walkOnce(t, root, count)
			if best[count] == 0 || took < best[count] {
				best[count] = took
			}
		}
	}

	for _, count := range workers {
		t.Logf("scan workers %3d: %s", count, best[count].Round(time.Millisecond))
	}
}

// walkOnce runs a full first backup of root into a store of its own.
func walkOnce(t *testing.T, root string, workers int) time.Duration {
	t.Helper()

	store := newMemStore()
	index := newMemIndex(store)

	walker := &Walker{
		Index:       index,
		Objects:     store,
		Set:         "bench",
		AgentID:     "bench",
		Root:        root,
		Workers:     8,
		ScanWorkers: workers,
	}

	start := time.Now()

	_, err := walker.Run(context.Background())
	require.NoError(t, err)

	return time.Since(start)
}

func benchInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}

	return value
}
