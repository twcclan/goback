package fix

import (
	"context"
	"log"

	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/urfave/cli"
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

	err := index.ReIndex(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}
