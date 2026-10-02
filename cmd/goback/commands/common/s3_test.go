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

func TestAnS3StoreKeepsIndexAndCacheFromTheS3Client(t *testing.T) {
	u := parse(t, testminio.Start(t, "goback")+"&index="+t.TempDir()+"&cache="+t.TempDir())

	store, err := initS3(u, cli.NewContext(nil, flag.NewFlagSet("goback", flag.ContinueOnError), nil))
	require.NoError(t, err)
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
