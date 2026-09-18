package pack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// served is a local archive storage that also hands out URLs into an HTTP
// server over the same directory, which is what a bucket does.
type served struct {
	*localArchiveStorage
	url string
}

func (s served) SignRange(_ context.Context, name string, offset, length int64, ttl time.Duration) (*proto.Location, error) {
	return &proto.Location{
		Url:     s.url + "/" + name,
		Header:  map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)},
		Length:  length,
		Expires: timestamppb.New(time.Now().Add(ttl)),
	}, nil
}

// follow fetches a location the way a caller would.
func follow(t *testing.T, location *proto.Location) *proto.Object {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, location.GetUrl(), nil)
	require.NoError(t, err)

	for name, value := range location.GetHeader() {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	require.Equal(t, http.StatusPartialContent, response.StatusCode)

	record, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Len(t, record, int(location.GetLength()))

	object, err := DecodeRecord(record)
	require.NoError(t, err)

	return object
}

// locatable returns a pack store whose archives can be fetched over HTTP,
// and a settle that closes it and hands back a reopened one, so the
// archives under test are committed rather than still being written.
func locatable(t *testing.T, extra ...PackOption) (*PackStorage, func() *PackStorage) {
	t.Helper()

	base := t.TempDir()
	server := httptest.NewServer(http.FileServer(http.Dir(base)))
	t.Cleanup(server.Close)

	options := append([]PackOption{
		WithArchiveStorage(served{localArchiveStorage: newLocal(base), url: server.URL}),
		WithArchiveIndex(NewInMemoryIndex()),
	}, extra...)

	store, err := NewPackStorage(options...)
	require.NoError(t, err)
	require.NoError(t, store.Open())

	return store, func() *PackStorage {
		require.NoError(t, store.Close())

		reopened, err := NewPackStorage(options...)
		require.NoError(t, err)
		require.NoError(t, reopened.Open())
		t.Cleanup(func() { _ = reopened.Close() })

		return reopened
	}
}

func TestReadAnswersWithALocation(t *testing.T) {
	ctx := context.Background()
	store, settle := locatable(t)

	blob := proto.NewObject(&proto.Blob{Data: []byte("save data")})
	require.NoError(t, store.Put(ctx, blob))

	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Length: 9, Ref: blob.Ref()}}})
	require.NoError(t, store.Put(ctx, file))

	store = settle()

	object, location, err := store.Read(ctx, file.Ref())
	require.NoError(t, err)
	require.Nil(t, object, "the bytes are not served when they can be addressed")
	require.NotNil(t, location)
	require.NotZero(t, location.GetExpires().AsTime())

	fetched := follow(t, location)
	require.True(t, fetched.Ref().Equal(file.Ref()), "the location yields the object asked for")
	require.EqualValues(t, 9, fetched.GetFile().GetParts()[0].GetLength())

	// a blob is never addressed, because an ordinary read would refuse it
	object, location, err = store.Read(ctx, blob.Ref())
	require.NoError(t, err)
	require.Nil(t, location)
	require.NotNil(t, object)
}

func TestReadServesTheBytesWhenItCannotAddressThem(t *testing.T) {
	ctx := context.Background()

	// sealed at rest, so the records are unreadable to anyone we could
	// send to the bucket
	store, settle := locatable(t, WithAtRestKey(atRestKey(t, "server")))

	file := proto.NewObject(&proto.File{Inline: []byte("small enough")})
	require.NoError(t, store.Put(ctx, file))

	store = settle()

	object, location, err := store.Read(ctx, file.Ref())
	require.NoError(t, err)
	require.Nil(t, location, "a record sealed at rest is never handed out")
	require.NotNil(t, object)
}

func TestReadWithoutASignerServesTheBytes(t *testing.T) {
	ctx := context.Background()

	store, err := NewPackStorage(WithArchiveStorage(newLocal(t.TempDir())), WithArchiveIndex(NewInMemoryIndex()))
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	file := proto.NewObject(&proto.File{Inline: []byte("small enough")})
	require.NoError(t, store.Put(ctx, file))

	object, location, err := store.Read(ctx, file.Ref())
	require.NoError(t, err)
	require.Nil(t, location)
	require.NotNil(t, object)
}
