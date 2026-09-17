// Package set manages backup sets.
package set

import (
	"log"

	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/urfave/cli"
)

var Command = cli.Command{
	Name:        "set",
	Description: "Manage backup sets",
	Subcommands: []cli.Command{
		{
			Name:        "delete",
			Description: "Close a set and retire its commits; --erase skips the trash window",
			ArgsUsage:   "<name>",
			Flags:       []cli.Flag{cli.BoolFlag{Name: "erase"}},
			Action:      deleteAction,
		},
		{
			Name:        "undelete",
			Description: "Reopen a set before its commits are tombstoned",
			ArgsUsage:   "<name>",
			Action:      undeleteAction,
		},
	},
}

func deleteAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: set delete [--erase] <name>")
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	err := common.GetRetention(index).DeleteSet(common.Context(c), c.Args().First(), c.Bool("erase"))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Set %s closed", c.Args().First())
	index.Close()
}

func undeleteAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: set undelete <name>")
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	err := common.GetRetention(index).UndeleteSet(common.Context(c), c.Args().First())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Set %s reopened", c.Args().First())
	index.Close()
}
