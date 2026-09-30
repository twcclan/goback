package repair

import (
	"context"
	"log"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

// Repairer is implemented by stores that can rewrite their damage away.
type Repairer interface {
	Repair(ctx context.Context) (*pack.RepairReport, error)
}

// Lister is implemented by indexes that can name their sets.
type Lister interface {
	ListSets(ctx context.Context) ([]index.SetInfo, error)
}

// Command is the repair command.
var Command = cli.Command{
	Name:        "repair",
	Description: "Rewrite every archive holding corrupt objects, leaving the corruption behind",
	Action:      repairAction,
}

func repairAction(c *cli.Context) {
	ctx := common.Context(c)

	store := common.GetObjectStore(c)
	defer common.CloseStore(store)

	idx := common.OpenIndex(c, store)

	repairer, ok := common.Unwrap(store).(Repairer)
	if !ok {
		log.Fatalf("storage %s cannot be repaired", c.GlobalString("storage"))
	}

	report, err := repairer.Repair(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Scrubbed %d objects in %d archives, rewrote %d", report.Scrub.Objects, report.Scrub.Archives, report.Archives)

	for _, ref := range report.Recovered {
		log.Printf("recovered %x from another archive", ref.GetHash())
	}

	for _, ref := range report.Lost {
		log.Printf("lost %x", ref.GetHash())
	}

	markDamage(ctx, idx, report.Lost)
}

// markDamage records the paths that held what the repair could not keep,
// so the next backup of their sets reads them again.
func markDamage(ctx context.Context, idx backup.Index, lost []*proto.Ref) {
	if len(lost) == 0 {
		return
	}

	damage, ok := idx.(backup.DamageIndex)
	lister, listable := idx.(Lister)

	if !ok || !listable {
		log.Printf("index %T cannot record damage, so nothing will read the lost objects again", idx)
		return
	}

	sets, err := lister.ListSets(ctx)
	if err != nil {
		log.Fatalf("Listing the sets failed: %v", err)
	}

	ids := make([]int64, 0, len(sets))
	names := make(map[int64]string, len(sets))
	for _, s := range sets {
		ids = append(ids, s.ID)
		names[s.ID] = s.Name
	}

	result, err := backup.ReportDamage(ctx, idx, damage, ids, lost)
	if err != nil {
		log.Fatalf("Recording the damage failed: %v", err)
	}

	for set, paths := range result.Paths {
		log.Printf("set %s: %d paths will be read again by the next backup", names[set], len(paths))
	}

	for _, at := range result.Lost {
		if at.Open {
			continue
		}

		log.Printf("set %s: %s is unrecoverable in the snapshots that hold this version", at.Set, at.Path)
	}

	if len(result.Rescan) > 0 {
		log.Printf("%d objects could not be placed at a path, so %d sets will be read in full", len(result.Unplaced), len(result.Rescan))
	}
}
