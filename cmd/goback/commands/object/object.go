package object

import (
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/codegangsta/cli"
)

var Command = cli.Command{
	Name:        "object",
	Description: "Do stuff with objects",
	Subcommands: []cli.Command{
		listCmd,
		getCmd,
	},
}

type object struct {
	store      backup.ObjectStore
	objectType proto.ObjectType
	ref        *proto.Ref
	out        string
}
