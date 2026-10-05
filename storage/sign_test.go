package storage_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/twcclan/goback/testing/tests3"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/pack"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/memblob"
)

// archive is the whole of one stored file, of which a location addresses
// a range.
var archive = []byte("0123456789abcdefghijklmnopqrstuvwxyz")

// signing is a bucket store over a real object store, holding one archive.
func signing(t *testing.T, name string) *storage.BucketStore {
	t.Helper()
	ctx := context.Background()

	bucket, err := blob.OpenBucket(ctx, tests3.Start(t, "goback"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = bucket.Close() })

	store := storage.NewBucketStore(bucket)

	file, err := store.Create(name)
	require.NoError(t, err)
	_, err = file.Write(archive)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	return store
}

// fetch follows a location, optionally overriding the headers it says to
// send, and returns the status and body.
func fetch(t *testing.T, url string, header map[string]string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	for name, value := range header {
		req.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return response.StatusCode, body
}

func TestASignedRangeReadsThoseBytesFromTheObjectStore(t *testing.T) {
	store := signing(t, "one.pack")

	location, err := store.SignRange(context.Background(), "one.pack", 10, 6, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 6, location.GetLength())
	require.WithinDuration(t, time.Now().Add(time.Minute), location.GetExpires().AsTime(), time.Minute)

	code, body := fetch(t, location.GetUrl(), location.GetHeader())
	require.Equal(t, http.StatusPartialContent, code)
	require.Equal(t, archive[10:16], body)
}

func TestASignedRangeCannotBeTurnedOnTheRestOfTheArchive(t *testing.T) {
	store := signing(t, "one.pack")

	location, err := store.SignRange(context.Background(), "one.pack", 10, 6, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "bytes=10-15", location.GetHeader()["Range"], "the range the caller must repeat")

	// the store refuses a request the signature does not cover; which of
	// the 4xx it answers with is its own business
	code, body := fetch(t, location.GetUrl(), nil)
	require.GreaterOrEqual(t, code, 400, "dropping the range reached %q", body)
	require.NotContains(t, string(body), string(archive), "the whole archive came back")

	code, body = fetch(t, location.GetUrl(), map[string]string{"Range": "bytes=0-35"})
	require.GreaterOrEqual(t, code, 400, "a widened range reached %q", body)
	require.NotContains(t, string(body), string(archive), "the whole archive came back")
}

func TestAnEmptyRangeIsNotSigned(t *testing.T) {
	store := signing(t, "one.pack")

	_, err := store.SignRange(context.Background(), "one.pack", 0, 0, time.Minute)
	require.ErrorIs(t, err, pack.ErrNoSignedURL)
}

func TestAStoreWithNoBytesToAddressSaysSo(t *testing.T) {
	ctx := context.Background()

	bucket, err := blob.OpenBucket(ctx, "mem://")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bucket.Close() })

	_, err = storage.NewBucketStore(bucket).SignRange(ctx, "one.pack", 0, 6, time.Minute)
	require.ErrorIs(t, err, pack.ErrNoSignedURL, "an in-memory bucket hands out no URLs")
}
