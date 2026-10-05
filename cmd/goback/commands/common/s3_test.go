package common

import (
	"bytes"
	"context"
	"flag"
	"io"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/internal/testminio"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli"
)

func TestAnS3StoreKeepsItsArchivesInTheIndexAndTakesACache(t *testing.T) {
	u := parse(t, testminio.Start(t, "goback")+"&cache="+t.TempDir())

	global := flag.NewFlagSet("goback", flag.ContinueOnError)
	global.String("index", t.TempDir(), "")

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
}
