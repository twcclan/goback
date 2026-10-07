package fix

import (
	"log"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/cmd/goback/commands/common/views"

	"github.com/urfave/cli"
)

// Command is the fix command.
var Command = cli.Command{
	Name:        "fix",
	Description: "Detect and fix problems",
	Action:      fixAction,
}

func fixAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	report, err := index.ReIndex(common.Context(c))
	if err != nil {
		common.Fatal(err)
	}

	common.CloseAll(store, index)

	common.Result(reindexed{Done: common.Done{Action: "reindexed"}, ReIndexView: common.View.ReIndex(report)}, func() {
		if report.Tied > 0 || report.Behind > 0 {
			log.Printf("%d commits shared their set's newest receipt time and were indexed a microsecond after it, %d were received before it and left out", report.Tied, report.Behind)
		}
	})
}

// reindexed is what fix prints.
type reindexed struct {
	common.Done
	views.ReIndexView
}
