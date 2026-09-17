package gc

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/pack"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

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
			Value: 21 * 24 * time.Hour,
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

func gcAction(c *cli.Context) {
	store := common.GetObjectStore(c)

	collector, ok := common.Unwrap(store).(pack.Collector)
	if !ok {
		log.Fatalf("storage %s cannot be garbage collected", c.GlobalString("storage"))
	}

	report, err := collector.Collect(context.Background(), pack.CollectOptions{
		Readers:      c.Int("readers"),
		DeadRatio:    c.Float64("dead-ratio"),
		ErasureBound: c.Duration("erasure-bound"),
		MinAge:       c.Duration("min-age"),
		NoSweep:      c.Bool("no-sweep"),
		TempDir:      c.String("temp-dir"),
	})
	if err != nil {
		log.Fatal(err)
	}

	Log(report)

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}

// Log prints one line per phase of a collection report.
func Log(report *pack.CollectReport) {
	for _, line := range strings.Split(Summary(report), "\n") {
		log.Print(line)
	}
}

// Summary renders a report as the lines Log prints.
func Summary(report *pack.CollectReport) string {
	summary := fmt.Sprintf("GC generation %d: %d roots, %d of %d objects in %d archives marked, %d objects (%s) dead, %s",
		report.Generation, report.Roots, report.Marked, report.Objects, report.Archives, report.DeadObjects, humanize.Bytes(report.DeadBytes), report.Duration.Round(time.Millisecond))

	if report.SweepSkipped != "" {
		return summary + "\nGC sweep skipped: " + report.SweepSkipped
	}

	return summary + fmt.Sprintf("\nGC swept %d archives (%d flagged for erasure), reclaimed %d objects (%s)", report.Swept, report.ErasedArchives, report.ReclaimedObjects, humanize.Bytes(report.ReclaimedBytes))
}
