package postgres

import (
	"fmt"

	"github.com/twcclan/goback/backup/postgres"

	"github.com/urfave/cli"
)

var archiveCmd = cli.Command{
	Name:      "archive",
	Usage:     "copy a finished WAL file into the spool; set archive_command = 'goback postgres archive --spool DIR %p %f'",
	ArgsUsage: "<path> <name>",
	Flags:     []cli.Flag{spoolFlag},
	Action:    archiveAction,
}

// archiveAction never touches the store, so Postgres never waits on it.
func archiveAction(c *cli.Context) error {
	if c.NArg() != 2 {
		return cli.NewExitError("usage: goback postgres archive --spool DIR <path> <name>", 2)
	}

	dir := c.String("spool")
	if dir == "" {
		return cli.NewExitError("--spool is required", 2)
	}

	if err := (postgres.Spool{Dir: dir}).Add(c.Args().Get(0), c.Args().Get(1)); err != nil {
		return cli.NewExitError(fmt.Sprintf("archiving %s: %v", c.Args().Get(1), err), 1)
	}

	return nil
}
