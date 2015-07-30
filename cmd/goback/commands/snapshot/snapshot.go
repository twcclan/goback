package snapshot

import (
	"github.com/twcclan/goback/backup"

	"github.com/codegangsta/cli"
)

type snapshot struct {
	backup   *backup.BackupWriter
	index    backup.Index
	base     string
	includes []string
	excludes []string
}

var Command = cli.Command{
	Name:        "snapshot",
	Description: "Manage your snapshots",
	Subcommands: []cli.Command{
		newCmd,
		listCmd,
	},
}
