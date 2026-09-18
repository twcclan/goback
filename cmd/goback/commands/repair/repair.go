package repair

import (
	"context"
	"log"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

// Repairer is implemented by stores that can rewrite their damage away.
type Repairer interface {
	Repair(ctx context.Context) (*pack.RepairReport, error)
}

// Command is the repair command.
var Command = cli.Command{
	Name:        "repair",
	Description: "Rewrite every archive holding corrupt objects, leaving the corruption behind",
	Action:      repairAction,
}

func repairAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	defer common.CloseStore(store)

	repairer, ok := common.Unwrap(store).(Repairer)
	if !ok {
		log.Fatalf("storage %s cannot be repaired", c.GlobalString("storage"))
	}

	report, err := repairer.Repair(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Scrubbed %d objects in %d archives, rewrote %d", report.Scrub.Objects, report.Scrub.Archives, report.Archives)

	for _, ref := range report.Recovered {
		log.Printf("recovered %x from another archive", ref.GetHash())
	}

	for _, ref := range report.Lost {
		log.Printf("lost %x; a backup of a source still holding it puts it back", ref.GetHash())
	}
}
