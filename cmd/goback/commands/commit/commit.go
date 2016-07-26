package commit

import (
	"github.com/twcclan/goback/backup"

	"github.com/codegangsta/cli"
)

type commit struct {
	backup   *backup.BackupWriter
	reader   *backup.BackupReader
	index    backup.Index
	base     string
	includes []string
	excludes []string
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
