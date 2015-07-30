package snapshot

import "github.com/codegangsta/cli"

func listAction(c *cli.Context) {

}

var listCmd = cli.Command{
	Name:        "list",
	Description: "List snapshots",
	Action:      listAction,
}
