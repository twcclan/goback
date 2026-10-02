package key

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twcclan/goback/backup/storekey"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli"
)

func TestIDPrintsTheKeysID(t *testing.T) {
	key, err := storekey.Generate("ops")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "store.key")
	require.NoError(t, key.Save(path))

	var out bytes.Buffer
	app := cli.NewApp()
	app.Writer = &out
	app.Commands = []cli.Command{Command}

	require.NoError(t, app.Run([]string{"goback", "key", "id", "--key", path}))
	require.Equal(t, key.IDString(), strings.TrimSpace(out.String()))
}
