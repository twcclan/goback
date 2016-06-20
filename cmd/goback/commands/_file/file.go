package file

import (
	"github.com/twcclan/goback/backup"

	"github.com/codegangsta/cli"
)

type file struct {
	reader *backup.BackupReader
	src    string
	dst    string
}

var Command = cli.Command{
	Name:        "file",
	Description: "Manage files",
	Subcommands: []cli.Command{
		restoreCmd,
	},
}
