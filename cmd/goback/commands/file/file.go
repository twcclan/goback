package file

import (
	"context"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"

	"github.com/urfave/cli"
)

type file struct {
	ctx      context.Context
	reader   *backup.BackupReader
	restorer *backup.Restorer
	store    backup.ObjectStore
	key      *storekey.Key
	index    backup.Index
	src      string
	dst      string
	when     time.Time
	set      string
}

var Command = cli.Command{
	Name:        "file",
	Description: "Manage files",
	Subcommands: []cli.Command{
		restoreCmd,
		showCmd,
	},
}
