// Package maintain runs a store's housekeeping: whatever is due, or one
// operation.
package maintain

import (
	"context"
	"log"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/cmd/goback/commands/gc"
	"github.com/twcclan/goback/storage/maintenance"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

// Command is the maintain command.
var Command = cli.Command{
	Name:        "maintain",
	Usage:       "Run the housekeeping that is due, as a store server does on its schedule; run it from cron",
	Description: "Without a subcommand: sweep, retire, compact and build presence filters, and garbage collect once the last collection is older than --gc-interval. A subcommand runs one operation",
	Flags: []cli.Flag{
		cli.DurationFlag{
			Name:  "gc-interval",
			Usage: "garbage collect when the last collection is at least this old; 0 never collects",
			Value: maintenance.DefaultSchedule.Collect,
		},
	},
	Action: action(due),
	Subcommands: []cli.Command{
		{
			Name:   "sweep",
			Usage:  "Finalize idle archives and end sessions whose lease ran out",
			Action: action(func(m *members) {
				m.store().Sweep(time.Now())
				common.Result(common.Done{Action: "swept"}, func() {})
			}),
		},
		{
			Name:  "compact",
			Usage: "Rewrite small archives into full-sized ones",
			Action: action(func(m *members) {
				if err := m.store().Compact(); err != nil {
					common.Fatal(err)
				}

				common.Result(common.Done{Action: "compacted"}, func() {})
			}),
		},
		{
			Name:  "retire",
			Usage: "Tombstone every retired commit past its window",
			Action: action(func(m *members) {
				retirer, ok := m.index.(backup.Retirer)
				if !ok {
					common.Fatalf("Index %T keeps no retention state", m.index)
				}

				n, err := retirer.Retire(common.Context(m.c), time.Now())
				if err != nil {
					common.Fatalf("Retirement failed after %d commits: %v", n, err)
				}

				common.Result(counted{"retired", n}, func() { log.Printf("Retired %d commits", n) })
			}),
		},
		{
			Name:  "presence",
			Usage: "Build the presence filter of every set whose newest commit has none",
			Action: action(func(m *members) {
				presence, ok := m.index.(maintenance.Presence)
				if !ok {
					common.Fatalf("Index %T keeps no presence filters", m.index)
				}

				n, err := presence.BuildPendingPresence(common.Context(m.c))
				if err != nil {
					common.Fatal(err)
				}

				common.Result(counted{"built_presence", n}, func() { log.Printf("Built %d presence filters", n) })
			}),
		},
		{
			Name:  "sizes",
			Usage: "Record the logical size of every commit that carries none",
			Action: action(func(m *members) {
				sizer, ok := m.index.(sizeFiller)
				if !ok {
					common.Fatalf("Index %T keeps no commit sizes", m.index)
				}

				n, err := sizer.FillMissingSizes(common.Context(m.c))
				if err != nil {
					common.Fatal(err)
				}

				common.Result(counted{"filled_sizes", n}, func() { log.Printf("Filled %d commit sizes", n) })
			}),
		},
	},
}

func due(m *members) {
	schedule := maintenance.DefaultSchedule
	schedule.Collect = m.c.Duration("gc-interval")

	base := common.Unwrap(m.objects)
	runner := &maintenance.Runner{Schedule: schedule}
	runner.Store, _ = base.(maintenance.Store)
	runner.Collector, _ = base.(pack.Collector)
	runner.Retirer, _ = m.index.(backup.Retirer)
	runner.Presence, _ = m.index.(maintenance.Presence)
	runner.Attributor, _ = m.index.(maintenance.Attributor)

	ran, err := runner.Due(common.Context(m.c))

	view := common.View.Maintenance(ran)
	if ran.Collected != nil {
		report := common.View.Report(ran.Collected)
		view.Collected = &report
	}

	common.Result(view, func() {
		log.Printf("Swept: %t, compacted: %t, retired %d commits, built %d presence filters", ran.Swept, ran.Compacted, ran.Retired, ran.Presence)

		if ran.Collected != nil {
			gc.Log(ran.Collected)
		} else {
			log.Print("No collection due")
		}
	})

	if err != nil {
		common.Fatal(err)
	}
}

// counted is what a maintenance operation did, and to how many things.
type counted struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// sizeFiller is an index that can work out the size of a commit it
// recorded without one.
type sizeFiller interface {
	FillMissingSizes(ctx context.Context) (int, error)
}

type members struct {
	c       *cli.Context
	objects backup.ObjectStore
	index   backup.Index
}

func (m *members) store() maintenance.Store {
	store, ok := common.Unwrap(m.objects).(maintenance.Store)
	if !ok {
		common.Fatalf("storage %s has no archives to maintain", m.c.GlobalString("storage"))
	}

	return store
}

func action(run func(*members)) func(*cli.Context) {
	return func(c *cli.Context) {
		objects := common.GetObjectStore(c)
		index := common.OpenIndex(c, objects)

		run(&members{c: c, objects: objects, index: index})

		index.Close()
		if cl, ok := objects.(common.Closer); ok {
			cl.Close()
		}
	}
}
