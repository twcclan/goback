package commit

import (
	"time"

	"github.com/twcclan/goback/backup"

	"github.com/urfave/cli"
	"go4.org/syncutil"
)

type commit struct {
	backup   *backup.BackupWriter
	reader   *backup.BackupReader
	index    backup.Index
	when     time.Time
	base     string
	from     string
	includes []string
	excludes []string
	gate     *syncutil.Gate
	set      string
}

var Command = cli.Command{
	Name:        "commit",
	Description: "Manage your commits",
	Subcommands: []cli.Command{
		newCmd,
		listCmd,
		restoreCmd,
	},
}
