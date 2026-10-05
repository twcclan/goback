package commit

import (
	"log"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/cmd/goback/commands/common/views"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func (c *commit) list() {
	commits, err := c.index.CommitInfo(c.ctx, c.set, time.Now(), 10)
	if err != nil {
		common.Fatal(errors.Wrap(err, "Failed reading commit info"))
	}

	out := make([]views.CommitView, len(commits))
	for i, commit := range commits {
		out[i] = common.View.Commit(commit)
	}

	common.Result(out, func() {
		for _, commit := range commits {
			note := ""
			if commit.Consistent {
				note = " consistent"
			}

			log.Printf("%s %x%s", time.Unix(commit.Timestamp, 0), commit.Tree.Hash, note)
		}
	})
}

func listAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	s := &commit{
		ctx:   common.Context(c),
		index: index,
		set:   c.GlobalString("set"),
	}

	s.list()

	common.CloseAll(store, index)
}

var listCmd = cli.Command{
	Name:        "list",
	Description: "List commits",
	Action:      listAction,
}
