package file

import "github.com/codegangsta/cli"

func (f *file) show() error {
	return nil
}

func showAction(c *cli.Context) {

}

var showCmd = cli.Command{
	Name:        "show",
	Description: "Show information about a file",
	Action:      showAction,
}
