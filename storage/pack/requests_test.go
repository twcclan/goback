package pack

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/proto"
	"github.com/gobackio/goback/storage/badger"

	"github.com/stretchr/testify/require"
)

// requestCounting counts what a bucket under the storage would be sent:
// an Open is an attributes request, the first read of an opened file and
// every ranged read a get, a closed writer a put.
type requestCounting struct {
	*memView

	mtx    sync.Mutex
	counts map[string]int
}

func newRequestCounting(bucket *memBucket) *requestCounting {
	return &requestCounting{memView: bucket.view(), counts: map[string]int{}}
}

func kindOf(name string) string {
	switch ext := path.Ext(name); ext {
	case ".goback":
		return "archive"
	case "":
		return "all"
	default:
		return ext
	}
}

func (c *requestCounting) count(op, name string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.counts[op+" "+kindOf(name)]++
}

// take returns the counts so far and starts over.
func (c *requestCounting) take() map[string]int {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	counts := c.counts
	c.counts = map[string]int{}

	return counts
}

func (c *requestCounting) Create(name string) (File, error) {
	file, err := c.memView.Create(name)
	if err != nil {
		return nil, err
	}

	return &countedFile{File: file, storage: c, name: name, writer: true}, nil
}

func (c *requestCounting) CreateNew(name string, data []byte) error {
	c.count("put", name)

	return c.memView.CreateNew(name, data)
}

func (c *requestCounting) Open(name string) (File, error) {
	file, err := c.memView.Open(name)

	// a file still being written is answered without asking the bucket
	if _, writing := file.(*memWriter); writing {
		return file, nil
	}

	c.count("head", name)

	if err != nil {
		return nil, err
	}

	return &countedFile{File: file, storage: c, name: name}, nil
}

func (c *requestCounting) OpenListed(listed ListedFile) (File, error) {
	file, err := c.memView.Open(listed.Name)
	if err != nil {
		return nil, err
	}

	return &countedFile{File: file, storage: c, name: listed.Name}, nil
}

func (c *requestCounting) List(extension string) ([]string, error) {
	c.count("list", extension)

	return c.memView.List(extension)
}

func (c *requestCounting) ListInfo(extension string) ([]ListedFile, error) {
	c.count("list", extension)

	return c.memView.ListInfo(extension)
}

func (c *requestCounting) Delete(name string) error {
	c.count("delete", name)

	return c.memView.Delete(name)
}

func (c *requestCounting) Checksum(name string) ([]byte, error) {
	c.count("head", name)

	file, err := c.memView.Open(name)
	if err != nil {
		return nil, err
	}

	sum := md5.New()
	_, err = io.Copy(sum, file)

	return sum.Sum(nil), err
}

type countedFile struct {
	File
	storage   *requestCounting
	name      string
	writer    bool
	streaming bool
}

func (f *countedFile) Read(p []byte) (int, error) {
	if !f.streaming {
		f.streaming = true
		f.storage.count("get", f.name)
	}

	return f.File.Read(p)
}

func (f *countedFile) Seek(offset int64, whence int) (int64, error) {
	f.streaming = false

	return f.File.Seek(offset, whence)
}

func (f *countedFile) ReadAt(p []byte, off int64) (int, error) {
	f.storage.count("get", f.name)

	return f.File.(io.ReaderAt).ReadAt(p, off)
}

func (f *countedFile) Close() error {
	if f.writer {
		f.storage.count("put", f.name)
	}

	return f.File.Close()
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	var out strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&out, "%s=%d ", key, counts[key])
	}

	return out.String()
}

// TestACommitAsksTheBucketForLittleBeyondWhatItWrites pins the requests a
// small commit that deduplicates against the one before it costs a store
// with a metadata and an index cache, as a server runs it.
func TestACommitAsksTheBucketForLittleBeyondWhatItWrites(t *testing.T) {
	bucket := newMemBucket()
	storage := newRequestCounting(bucket)

	cache, err := badger.New(t.TempDir())
	require.NoError(t, err)

	store, err := NewPackStorage(
		WithArchiveStorage(storage),
		WithArchiveIndex(NewInMemoryIndex()),
		WithMetadataCache(cache),
		WithIndexCache(t.TempDir()),
		WithCloseBeforeRead(true),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	previous := makeChain(makeTestData(t, 10))
	commit := func(objects []*proto.Object) {
		ctx, err := store.BeginSession(context.Background(), &backup.Session{AgentID: "agent", Set: "world"})
		require.NoError(t, err)

		for _, obj := range objects {
			require.NoError(t, store.Put(ctx, obj))
		}
	}

	commit(previous)
	storage.take()

	// one new file beside the trees the previous commit stored
	blob := makeTestData(t, 1)
	file := makeGCFiles(blob)
	root := treeOf(append(file, previous[len(previous)-3]))
	commit(append(append(blob, file...), root, proto.NewObject(&proto.Commit{Tree: root.Ref(), Timestamp: 2, BackupSet: "world"})))

	// two archives: the session's, then the one its commit's un-tombstone
	// is flushed in before the seals are read
	most := map[string]int{
		"put archive": 2, "put .idx": 2, "put .committed": 2, "put .begin": 1, "put .end": 1,
		// what an upload kept, and when the storage stamped each index
		"head archive": 2, "head .idx": 2,
		// the seals at begin and at commit
		"list .seal": 2,
	}

	counts := storage.take()
	for request, n := range counts {
		require.LessOrEqualf(t, n, most[request], "%s in %s", request, formatCounts(counts))
	}
}
