package pack

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// countingStorage counts the reads of archive files, so a test can see how
// many the mark took.
type countingStorage struct {
	*localArchiveStorage

	mu    sync.Mutex
	reads int
}

func (c *countingStorage) Open(name string) (File, error) {
	file, err := c.localArchiveStorage.Open(name)
	if err != nil || !strings.HasSuffix(name, ArchiveSuffix) {
		return file, err
	}

	return &countingFile{File: file, storage: c}, nil
}

func (c *countingStorage) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.reads
}

type countingFile struct {
	File
	storage *countingStorage
}

func (c *countingFile) ReadAt(p []byte, off int64) (int, error) {
	c.storage.mu.Lock()
	c.storage.reads++
	c.storage.mu.Unlock()

	return c.File.(io.ReaderAt).ReadAt(p, off)
}

func TestMarkReadsNeighboursTogether(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	storage := &countingStorage{localArchiveStorage: newLocal(base)}

	open := func() *PackStorage {
		store, err := NewPackStorage(WithArchiveStorage(storage), WithArchiveIndex(NewInMemoryIndex()))
		require.NoError(t, err)
		require.NoError(t, store.Open())

		return store
	}

	store := open()

	const files = 60

	children := make([]*proto.Object, 0, files)
	for i := range files {
		file := proto.NewObject(&proto.File{Inline: []byte(fmt.Sprintf("file %d", i))})
		require.NoError(t, store.Put(ctx, file))
		children = append(children, file)
	}

	tree := treeOf(children)
	require.NoError(t, store.Put(ctx, tree))

	commit := proto.NewObject(&proto.Commit{Tree: tree.Ref(), Timestamp: 1, BackupSet: "world"})
	require.NoError(t, store.Put(ctx, commit))
	require.NoError(t, store.Close())

	store = open()
	t.Cleanup(func() { _ = store.Close() })

	before := storage.count()

	report, err := store.Collect(ctx, CollectOptions{NoSweep: true, TempDir: t.TempDir()})
	require.NoError(t, err)
	require.EqualValues(t, files+2, report.Marked, "every object of the commit is reachable")

	reads := storage.count() - before
	require.Less(t, reads, files/2, "neighbouring records come back in one read, not one each")
}

func TestScanIndexYieldsTheStoredRecords(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()

	options := []PackOption{WithArchiveStorage(newLocal(base)), WithArchiveIndex(NewInMemoryIndex())}

	store, err := NewPackStorage(options...)
	require.NoError(t, err)
	require.NoError(t, store.Open())

	for i := range 20 {
		require.NoError(t, store.Put(ctx, proto.NewObject(&proto.Blob{Data: []byte(fmt.Sprintf("blob %d", i))})))
	}
	require.NoError(t, store.Close())

	store, err = NewPackStorage(options...)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	require.NotEmpty(t, store.archives)
	archive := store.archives[0]

	loaded, err := archive.getIndex()
	require.NoError(t, err)
	require.NotEmpty(t, loaded)

	var streamed IndexFile
	require.NoError(t, scanArchive(archive, func(_ int, rec *IndexRecord) error {
		streamed = append(streamed, *rec)

		return nil
	}, nil, nil))

	require.Equal(t, loaded, streamed, "streaming an index gives what loading it does")
}
