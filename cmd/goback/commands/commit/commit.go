package commit

import (
	"context"
	"time"

	"github.com/twcclan/goback/backup"

	"github.com/urfave/cli"
)

type commit struct {
	ctx      context.Context
	reader   *backup.BackupReader
	restorer *backup.Restorer
	store    backup.ObjectStore
	index    backup.Index
	when     time.Time
	base     string
	from     string
	set      string
	delete   bool
}

var Command = cli.Command{
	Name:        "commit",
	Description: "Manage your commits",
	Subcommands: []cli.Command{
		newCmd,
		listCmd,
		restoreCmd,
		deleteCmd,
		undeleteCmd,
	},
}
