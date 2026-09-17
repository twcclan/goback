package fix

import (
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
	index := common.OpenIndex(c, store)

	err := index.ReIndex(common.Context(c))
	if err != nil {
		log.Fatal(err)
	}

	index.Close()

	common.CloseStore(store)
}
