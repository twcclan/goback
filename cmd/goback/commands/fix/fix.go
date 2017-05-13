package fix

import (
	"log"

	"github.com/codegangsta/cli"
	"github.com/twcclan/goback/cmd/goback/commands/common"
)

var Command = cli.Command{
	Name:        "fix",
	Description: "Detect and fix problems",
	Action:      fixAction,
}

func fixAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}
