package common

import (
	"bytes"
	"context"
	"flag"
	"io"
	"path/filepath"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/testing/tests3"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli"
)

func TestAnS3StoreKeepsItsArchivesInTheIndexAndCachesUnderTheCacheDir(t *testing.T) {
	location := tests3.Start(t, "goback")
	u := parse(t, location)
	cacheDir := t.TempDir()

	global := flag.NewFlagSet("goback", flag.ContinueOnError)
	global.String("index", t.TempDir(), "")
	global.String("storage", location, "")
	global.String("cache-dir", cacheDir, "")

	store, err := initS3(u, cli.NewContext(nil, flag.NewFlagSet("s3", flag.ContinueOnError), cli.NewContext(nil, global, nil)))
	require.NoError(t, err)
	require.Contains(t, storeIndexes, store)
	t.Cleanup(func() { CloseStore(store) })

	ctx := context.Background()
	content := bytes.Repeat([]byte("goback"), 1<<16)

	ref, err := backup.PutFile(ctx, store, nil, int64(len(content)), bytes.NewReader(content))
	require.NoError(t, err)

	reader, err := backup.NewBackupReader(store).ReadFile(ctx, ref)
	require.NoError(t, err)

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, got)

	metadata, err := filepath.Glob(filepath.Join(cacheDir, "stores", "*", "metadata"))
	require.NoError(t, err)
	require.Len(t, metadata, 1, "the bucket's metadata cache sits under --cache-dir")
}

func TestABucketURLTakesNoCache(t *testing.T) {
	global := flag.NewFlagSet("goback", flag.ContinueOnError)
	global.String("index", t.TempDir(), "")

	_, err := initS3(parse(t, "s3://bucket?cache="+t.TempDir()), cli.NewContext(nil, flag.NewFlagSet("s3", flag.ContinueOnError), cli.NewContext(nil, global, nil)))
	require.ErrorContains(t, err, "--cache-dir")
}
