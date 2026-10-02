package backup

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// smallSplits shrinks the split bounds so a test file of a few hundred
// parts builds a tree several levels deep.
func smallSplits(t *testing.T) {
	min, avg, max := splitMin, splitAvg, splitMax
	splitMin, splitAvg, splitMax = 2, 4, 8

	t.Cleanup(func() { splitMin, splitAvg, splitMax = min, avg, max })
}

// byteParts stores one blob per byte of content and returns the parts.
func byteParts(t *testing.T, store ObjectStore, content []byte) []*proto.FilePart {
	t.Helper()

	parts := make([]*proto.FilePart, len(content))
	for i, b := range content {
		blob := proto.NewObject(&proto.Blob{Data: []byte{b, byte(i >> 8), byte(i)}[:1]})
		require.NoError(t, store.Put(context.Background(), blob))

		parts[i] = &proto.FilePart{Offset: uint64(i), Length: 1, Ref: blob.Ref()}
	}

	return parts
}

func treeFile(t *testing.T, store ObjectStore, content []byte) *proto.File {
	t.Helper()

	ref, err := PutParts(context.Background(), store, nil, byteParts(t, store, content))
	require.NoError(t, err)

	obj, err := store.Get(context.Background(), ref)
	require.NoError(t, err)

	return obj.GetFile()
}

func patterned(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*7 + i/251)
	}

	return data
}

func TestALargeFileIsATreeThatReadsBackWhole(t *testing.T) {
	smallSplits(t)

	store := newMemStore()
	want := patterned(600)
	file := treeFile(t, store, want)

	require.Greater(t, file.GetSplitDepth(), uint32(1), "600 parts in runs of at most 8 nest")
	require.Len(t, file.GetSplitLengths(), len(file.GetSplits()))

	var got bytes.Buffer
	_, err := newFileReader(context.Background(), store, file, nil).WriteTo(&got)
	require.NoError(t, err)
	require.Equal(t, want, got.Bytes())

	parts, err := FileParts(context.Background(), store, file)
	require.NoError(t, err)
	require.Len(t, parts, len(want))
}

func TestASeekReadsTheRightBytesAnywhere(t *testing.T) {
	smallSplits(t)

	store := newMemStore()
	want := patterned(600)
	reader := newFileReader(context.Background(), store, treeFile(t, store, want), nil)

	for _, at := range []int64{599, 0, 300, 7, 8, 512, 598} {
		_, err := reader.Seek(at, io.SeekStart)
		require.NoError(t, err)

		got := make([]byte, 2)
		n, err := io.ReadFull(reader, got)
		if at == 599 {
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		} else {
			require.NoError(t, err)
		}

		require.Equal(t, want[at:at+int64(n)], got[:n], "at %d", at)
	}
}

// fileCountingStore counts the file objects read through it.
type fileCountingStore struct {
	*memStore
	files atomic.Int32
}

func (c *fileCountingStore) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	obj, err := c.memStore.Get(ctx, ref)
	if obj.GetFile() != nil {
		c.files.Add(1)
	}

	return obj, err
}

func TestARandomReadLoadsOnlyTheBranchItIsIn(t *testing.T) {
	smallSplits(t)

	store := &fileCountingStore{memStore: newMemStore()}
	file := treeFile(t, store, patterned(600))
	store.files.Store(0)

	reader := newFileReader(context.Background(), store, file, nil)
	_, err := reader.Seek(-1, io.SeekEnd)
	require.NoError(t, err)

	_, err = reader.Read(make([]byte, 1))
	require.NoError(t, err)
	require.Equal(t, int32(file.GetSplitDepth()), store.files.Load(), "one file object per level below the root")
}

func TestAnEditEarlyInAFileKeepsMostOfItsFileObjects(t *testing.T) {
	smallSplits(t)

	store := newMemStore()
	before := patterned(600)
	after := append([]byte{0xee}, before...)

	objects := func(file *proto.File) map[string]bool {
		held := map[string]bool{}
		require.NoError(t, SubFiles(context.Background(), store, file, func(ref *proto.Ref) error {
			held[string(ref.GetHash())] = true
			return nil
		}))

		return held
	}

	was, is := objects(treeFile(t, store, before)), objects(treeFile(t, store, after))

	kept := 0
	for ref := range is {
		if was[ref] {
			kept++
		}
	}

	require.Greater(t, kept, len(is)*3/4, "only the objects around the edit change")
}

func TestAFileSplitBeforeSplitsWereMeasuredStillReads(t *testing.T) {
	store := newMemStore()
	want := patterned(20)
	parts := byteParts(t, store, want)

	first := proto.NewObject(&proto.File{Parts: parts[:12]})
	second := proto.NewObject(&proto.File{Parts: parts[12:]})
	require.NoError(t, store.Put(context.Background(), first))
	require.NoError(t, store.Put(context.Background(), second))

	file := &proto.File{Splits: []*proto.Ref{first.Ref(), second.Ref()}}
	reader := newFileReader(context.Background(), store, file, nil)

	_, err := reader.Seek(15, io.SeekStart)
	require.NoError(t, err)

	got := make([]byte, 5)
	_, err = io.ReadFull(reader, got)
	require.NoError(t, err)
	require.Equal(t, want[15:], got)
}
