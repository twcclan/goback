package snapshot

import (
	"fmt"
	"log"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/codegangsta/cli"
)

func listAction(c *cli.Context) {
	idx := common.GetIndex(c)

	err := idx.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer idx.Close()

	snapshots, err := idx.SnapshotInfo(time.Now(), 10)
	for i, pointer := range snapshots {
		fmt.Printf("#%d %x %v\n", i, pointer.Ref.Sum, time.Unix(pointer.Info.Timestamp, 0))
	}
}

var listCmd = cli.Command{
	Name:        "list",
	Description: "List snapshots",
	Action:      listAction,
}
