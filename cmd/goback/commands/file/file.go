package file

import (
	"time"

	"github.com/twcclan/goback/backup"

	"github.com/urfave/cli"
)

type file struct {
	reader *backup.BackupReader
	index  backup.Index
	src    string
	dst    string
	when   time.Time
	set    string
}

var Command = cli.Command{
	Name:        "file",
	Description: "Manage files",
	Subcommands: []cli.Command{
		restoreCmd,
		showCmd,
	},
}
