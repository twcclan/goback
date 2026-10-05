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

	common.Result(common.View.Report(report), func() { Log(report) })

	common.CloseStore(store)
}

// Log prints one line per phase of a collection report.
func Log(report *pack.CollectReport) {
	for _, line := range strings.Split(report.Summary(), "\n") {
		log.Print(line)
	}
}
