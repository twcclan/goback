// Package set manages backup sets.
package set

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/twcclan/goback/admin"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/urfave/cli"
)

// Command is the set command.
var Command = cli.Command{
	Name:        "set",
	Description: "Manage backup sets",
	Subcommands: []cli.Command{
		{
			Name:        "delete",
			Description: "Close a set and retire its commits; --erase skips the trash window",
			ArgsUsage:   "<name>",
			Flags:       []cli.Flag{cli.BoolFlag{Name: "erase"}},
			Action:      deleteAction,
		},
		{
			Name:        "undelete",
			Description: "Reopen a set before its commits are tombstoned",
			ArgsUsage:   "<name>",
			Action:      undeleteAction,
		},
		{
			Name:        "retention",
			Description: "Show or change the retention of a set, or the store's defaults without a name; a keep flag replaces the policy, --inherit drops it",
			ArgsUsage:   "[<name>]",
			Flags: []cli.Flag{
				cli.IntFlag{Name: "keep-last"},
				cli.IntFlag{Name: "keep-hourly"},
				cli.IntFlag{Name: "keep-daily"},
				cli.IntFlag{Name: "keep-weekly"},
				cli.IntFlag{Name: "keep-monthly"},
				cli.DurationFlag{Name: "keep-within"},
				cli.BoolFlag{Name: "inherit", Usage: "drop the policy: a set inherits the store's, the store uses the built-in"},
				cli.IntFlag{Name: "hold-days", Value: -1, Usage: "store only: days a commit retired by policy waits for its tombstone"},
				cli.IntFlag{Name: "trash-days", Value: -1, Usage: "store only: days a deleted commit waits for its tombstone"},
			},
			Action: retentionAction,
		},
	},
}

var keepFlags = []string{"keep-last", "keep-hourly", "keep-daily", "keep-weekly", "keep-monthly", "keep-within"}

func retentionAction(c *cli.Context) {
	if c.NArg() > 1 {
		log.Fatal("usage: set retention [<name>] [--keep-... | --inherit] [--hold-days N] [--trash-days N]")
	}

	store := common.GetObjectStore(c)
	idx := common.OpenIndex(c, store)
	defer idx.Close()

	x, ok := idx.(admin.Index)
	if !ok {
		log.Fatalf("Index %T keeps no retention settings", idx)
	}

	ctx := common.Context(c)
	name := c.Args().First()

	var policy *retention.Policy
	for _, flag := range keepFlags {
		if c.IsSet(flag) {
			policy = &retention.Policy{
				KeepLast:    c.Int("keep-last"),
				KeepHourly:  c.Int("keep-hourly"),
				KeepDaily:   c.Int("keep-daily"),
				KeepWeekly:  c.Int("keep-weekly"),
				KeepMonthly: c.Int("keep-monthly"),
				KeepWithin:  c.Duration("keep-within"),
			}
			break
		}
	}

	if policy != nil || c.Bool("inherit") {
		var err error
		if name == "" {
			err = x.SetDefaultPolicy(ctx, policy)
		} else {
			err = x.SetPolicy(ctx, name, policy)
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	if c.Int("hold-days") >= 0 || c.Int("trash-days") >= 0 {
		if name != "" {
			log.Fatal("the hold and trash windows belong to the store; leave the set name out")
		}

		w, err := x.Windows(ctx)
		if err != nil {
			log.Fatal(err)
		}

		if days := c.Int("hold-days"); days >= 0 {
			w.HoldDays = days
		}

		if days := c.Int("trash-days"); days >= 0 {
			w.TrashDays = days
		}

		if err := x.SetWindows(ctx, w); err != nil {
			log.Fatal(err)
		}
	}

	showRetention(ctx, x, name)
}

func showRetention(ctx context.Context, x admin.Index, name string) {
	w, err := x.Windows(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if name == "" {
		policy, stored, err := x.GetDefaultPolicy(ctx)
		if err != nil {
			log.Fatal(err)
		}

		source := "built-in"
		if stored {
			source = "stored"
		}

		fmt.Printf("store default: %s (%s)\nhold %d days, trash %d days\n", describe(policy), source, w.HoldDays, w.TrashDays)

		return
	}

	ret, err := x.GetPolicy(ctx, name)
	if err != nil {
		log.Fatal(err)
	}

	source := "inherited"
	if ret.Policy != nil {
		source = "own"
	}

	fmt.Printf("set %s: %s (%s)\n", name, describe(ret.Effective), source)
	if ret.Paused {
		fmt.Println("retirement is paused since the rebuild; set a policy to resume it")
	}
}

// describe prints a policy as its keep flags.
func describe(p retention.Policy) string {
	var parts []string
	for _, f := range []struct {
		name  string
		count int
	}{
		{"keep-last", p.KeepLast}, {"keep-hourly", p.KeepHourly}, {"keep-daily", p.KeepDaily},
		{"keep-weekly", p.KeepWeekly}, {"keep-monthly", p.KeepMonthly},
	} {
		if f.count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", f.name, f.count))
		}
	}

	if p.KeepWithin > 0 {
		within := p.KeepWithin.String()
		if p.KeepWithin == time.Duration(1<<63-1) {
			within = "forever"
		}

		parts = append(parts, "keep-within="+within)
	}

	if len(parts) == 0 {
		return "keep nothing"
	}

	return strings.Join(parts, " ")
}

func deleteAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: set delete [--erase] <name>")
	}

	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	err := common.GetRetention(index).DeleteSet(common.Context(c), c.Args().First(), c.Bool("erase"))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Set %s closed", c.Args().First())
	index.Close()
}

func undeleteAction(c *cli.Context) {
	if c.NArg() != 1 {
		log.Fatal("usage: set undelete <name>")
	}

	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	err := common.GetRetention(index).UndeleteSet(common.Context(c), c.Args().First())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Set %s reopened", c.Args().First())
	index.Close()
}
