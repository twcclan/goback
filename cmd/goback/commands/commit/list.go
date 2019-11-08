package commit

import (
	"context"
	"log"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func (c *commit) list() {
	commits, err := c.index.CommitInfo(context.Background(), time.Now(), 10)
	if err != nil {
		log.Fatal(errors.Wrap(err, "Failed reading commit info"))
	}

	for _, commit := range commits {
		log.Printf("%s %x", time.Unix(commit.Timestamp, 0), commit.Tree.Sha1)
	}
}

func listAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	s := &commit{
		index: index,
	}

	s.list()

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}

var listCmd = cli.Command{
	Name:        "list",
	Description: "List commits",
	Action:      listAction,
}
