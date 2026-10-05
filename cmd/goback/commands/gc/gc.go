package gc

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

// Command is the gc command.
var Command = cli.Command{
	Name:        "gc",
	Description: "Mark every object reachable from a live commit or pin and rewrite archives whose dead share justifies it",
	Action:      gcAction,
	Flags: []cli.Flag{
		cli.IntFlag{
			Name:  "readers",
			Usage: "parallel object reads during the mark",
			Value: 32,
		},
		cli.Float64Flag{
			Name:  "dead-ratio",
			Usage: "rewrite an archive once this share of its bytes is dead",
			Value: 0.3,
		},
		cli.DurationFlag{
			Name:  "erasure-bound",
			Usage: "rewrite an archive that has held dead objects this long regardless of ratio",
			Value: 14 * 24 * time.Hour,
		},
		cli.DurationFlag{
			Name:  "min-age",
			Usage: "never drop an object younger than this",
			Value: 24 * time.Hour,
		},
		cli.BoolFlag{
			Name:  "no-sweep",
			Usage: "mark only",
		},
		cli.StringFlag{
			Name:  "temp-dir",
			Usage: "directory for the live-set runs; defaults to the system temp dir",
		},
	},
}

// attributer is an index that names the set behind a root, so a
// collection can record what each set's objects take up.
type attributer interface {
	RootOwner(ctx context.Context) (func(root []byte) pack.Attribution, error)
	RecordSetSizes(ctx context.Context, report *pack.CollectReport) error
}

func gcAction(c *cli.Context) {
	ctx := common.Context(c)
	store := common.GetObjectStore(c)

	collector, ok := common.Unwrap(store).(pack.Collector)
	if !ok {
		common.Fatalf("storage %s cannot be garbage collected", c.GlobalString("storage"))
	}

	index := common.OpenIndex(c, store)
	defer index.Close()

	sizes, _ := index.(attributer)

	opts := pack.CollectOptions{
		Readers:      c.Int("readers"),
		DeadRatio:    c.Float64("dead-ratio"),
		ErasureBound: c.Duration("erasure-bound"),
		MinAge:       c.Duration("min-age"),
		NoSweep:      c.Bool("no-sweep"),
		TempDir:      c.String("temp-dir"),
	}

	if sizes != nil {
		owner, err := sizes.RootOwner(ctx)
		if err != nil {
			common.Fatal(err)
		}

		opts.Owner = owner
	}

	report, err := collector.Collect(ctx, opts)
	if err != nil {
		common.Fatal(err)
	}

	if sizes != nil {
		if err := sizes.RecordSetSizes(ctx, report); err != nil {
			common.Fatal(err)
		}
	}

	common.Result(View(report), func() { Log(report) })

	common.CloseStore(store)
}

// ReportView is a collection report as JSON output shows it; the per-set
// maps are keyed by set id.
type ReportView struct {
	Generation       uint64           `json:"generation"`
	Waiting          uint64           `json:"waiting,omitempty"`
	Archives         int              `json:"archives"`
	Objects          uint64           `json:"objects"`
	Roots            int              `json:"roots"`
	Marked           uint64           `json:"marked"`
	DeadObjects      uint64           `json:"dead_objects"`
	DeadBytes        uint64           `json:"dead_bytes"`
	Resumed          int              `json:"resumed"`
	ErasedArchives   int              `json:"erased_archives"`
	Condemned        int              `json:"condemned"`
	SweepSkipped     string           `json:"sweep_skipped,omitempty"`
	Published        int              `json:"published"`
	Swept            int              `json:"swept"`
	ReclaimedObjects uint64           `json:"reclaimed_objects"`
	ReclaimedBytes   uint64           `json:"reclaimed_bytes"`
	CopiedBytes      uint64           `json:"copied_bytes"`
	Purged           int              `json:"purged"`
	Seconds          float64          `json:"seconds"`
	SetBytes         map[int64]uint64 `json:"set_bytes,omitempty"`
	SetDeduplicated  map[int64]uint64 `json:"set_deduplicated,omitempty"`
	SetAlone         map[int64]uint64 `json:"set_alone,omitempty"`
	SetExclusive     map[int64]uint64 `json:"set_exclusive,omitempty"`
}

// View is report as JSON output shows it.
func View(report *pack.CollectReport) ReportView {
	return ReportView{Generation: report.Generation, Waiting: report.Waiting, Archives: report.Archives, Objects: report.Objects,
		Roots: report.Roots, Marked: report.Marked, DeadObjects: report.DeadObjects, DeadBytes: report.DeadBytes,
		Resumed: report.Resumed, ErasedArchives: report.ErasedArchives, Condemned: report.Condemned,
		SweepSkipped: report.SweepSkipped, Published: report.Published, Swept: report.Swept,
		ReclaimedObjects: report.ReclaimedObjects, ReclaimedBytes: report.ReclaimedBytes, CopiedBytes: report.CopiedBytes, Purged: report.Purged,
		Seconds: report.Duration.Seconds(), SetBytes: report.SetBytes, SetDeduplicated: report.SetDeduplicated,
		SetAlone: report.SetAlone, SetExclusive: report.SetExclusive}
}

// Log prints one line per phase of a collection report.
func Log(report *pack.CollectReport) {
	for _, line := range strings.Split(report.Summary(), "\n") {
		log.Print(line)
	}
}

