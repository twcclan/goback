// Package maintain runs one housekeeping operation against a store.
package maintain

import (
	"log"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/maintenance"

	"github.com/urfave/cli"
)

// Command is the maintain command.
var Command = cli.Command{
	Name:        "maintain",
	Description: "Run one maintenance operation against the store, as the server does on its schedule",
	Subcommands: []cli.Command{
		{
			Name:   "sweep",
			Usage:  "Finalize idle archives and end sessions whose lease ran out",
			Action: action(func(m *members) { m.store().Sweep(time.Now()) }),
		},
		{
			Name:  "compact",
			Usage: "Rewrite small archives into full-sized ones",
			Action: action(func(m *members) {
				if err := m.store().Compact(); err != nil {
					log.Fatal(err)
				}
			}),
		},
		{
			Name:  "retire",
			Usage: "Tombstone every retired commit past its window",
			Action: action(func(m *members) {
				retirer, ok := m.index.(backup.Retirer)
				if !ok {
					log.Fatalf("Index %T keeps no retention state", m.index)
				}

				n, err := retirer.Retire(common.Context(m.c), time.Now())
				if err != nil {
					log.Fatalf("Retirement failed after %d commits: %v", n, err)
				}

				log.Printf("Retired %d commits", n)
			}),
		},
		{
			Name:  "presence",
			Usage: "Build the presence filter of every set whose newest commit has none",
			Action: action(func(m *members) {
				presence, ok := m.index.(maintenance.Presence)
				if !ok {
					log.Fatalf("Index %T keeps no presence filters", m.index)
				}

				n, err := presence.BuildPendingPresence(common.Context(m.c))
				if err != nil {
					log.Fatal(err)
				}

				log.Printf("Built %d presence filters", n)
			}),
		},
	},
}

type members struct {
	c       *cli.Context
	objects backup.ObjectStore
	index   backup.Index
}

func (m *members) store() maintenance.Store {
	store, ok := common.Unwrap(m.objects).(maintenance.Store)
	if !ok {
		log.Fatalf("storage %s has no archives to maintain", m.c.GlobalString("storage"))
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
