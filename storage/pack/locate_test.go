package pack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
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

// fetch returns the bytes a location addresses.
func fetch(t *testing.T, location *proto.Location) []byte {
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

	return record
}

// follow fetches a location the way a caller would.
func follow(t *testing.T, location *proto.Location) *proto.Object {
	t.Helper()

	object, err := DecodeRecord(fetch(t, location), location.GetAtRestKey())
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

	current := store

	return store, func() *PackStorage {
		require.NoError(t, current.Close())

		reopened, err := NewPackStorage(options...)
		require.NoError(t, err)
		require.NoError(t, reopened.Open())
		t.Cleanup(func() { _ = reopened.Close() })
		current = reopened

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

func TestALocationCarriesTheKeyToItsArchiveOnly(t *testing.T) {
	ctx := context.Background()
	key := atRestKey(t)
	store, settle := locatable(t, WithAtRestKey(key))

	first := proto.NewObject(&proto.File{Inline: []byte("first archive")})
	require.NoError(t, store.Put(ctx, first))
	store = settle()

	second := proto.NewObject(&proto.File{Inline: []byte("second archive")})
	require.NoError(t, store.Put(ctx, second))
	store = settle()

	_, one, err := store.Read(ctx, first.Ref())
	require.NoError(t, err)
	require.NotNil(t, one, "a record sealed at rest is handed out with its archive's key")
	require.NotEmpty(t, one.GetAtRestKey())

	_, two, err := store.Read(ctx, second.Ref())
	require.NoError(t, err)
	require.NotNil(t, two)

	record := fetch(t, one)
	require.NotContains(t, string(record), "first archive", "the bucket holds the record sealed")
	require.True(t, follow(t, one).Ref().Equal(first.Ref()))
	require.True(t, follow(t, two).Ref().Equal(second.Ref()))

	_, err = DecodeRecord(fetch(t, two), one.GetAtRestKey())
	require.Error(t, err, "one archive's key does not open another")

	_, err = DecodeRecord(record, nil)
	require.ErrorIs(t, err, ErrAtRestKeyMissing)

	// the store's own key is not what a location carries
	var whole bytes.Buffer
	require.NoError(t, insecurecleartextkeyset.Write(key.handle, keyset.NewBinaryWriter(&whole)))
	require.NotEqual(t, whole.Bytes(), one.GetAtRestKey())
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
