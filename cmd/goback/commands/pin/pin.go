// Package pin manages pins: objects that keep a commit, tree or file
// alive regardless of retention.
package pin

import (
	"log"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/urfave/cli"
)

var Command = cli.Command{
	Name:        "pin",
	Description: "Keep commits, trees or files regardless of retention",
	Subcommands: []cli.Command{
		{
			Name:        "add",
			Description: "Pin a ref",
			ArgsUsage:   "<ref>",
			Action:      addAction,
		},
		{
			Name:        "list",
			Description: "List the live pins",
			Action:      listAction,
		},
		{
			Name:        "remove",
			Description: "Tombstone a pin",
			ArgsUsage:   "<pin-ref>",
			Action:      removeAction,
		},
	},
}

func addAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: pin add <ref>")
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	pin := proto.NewObject(&proto.Pin{Target: common.ParseRef(c.Args().First())})

	err := index.Put(common.Context(c), pin)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Pinned %s as %x", c.Args().First(), pin.Ref().Hash)
	index.Close()
}

func listAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	pins, err := common.GetRetention(index).Pins(common.Context(c))
	if err != nil {
		log.Fatal(err)
	}

	for _, pin := range pins {
		log.Printf("%x -> %x (%s)", pin.Ref.Hash, pin.Target.Hash, time.Unix(0, pin.ReceivedAtNs).Format(time.RFC3339))
	}

	index.Close()
}

func removeAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: pin remove <pin-ref>")
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	err := common.GetRetention(index).Unpin(common.Context(c), common.ParseRef(c.Args().First()))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Pin %s removed", c.Args().First())
	index.Close()
}
