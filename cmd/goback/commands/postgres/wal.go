package postgres

import (
	"log"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/postgres"
	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/urfave/cli"
)

var baseSetFlag = cli.StringFlag{
	Name:  "base-set",
	Usage: "set the cluster's base backups go to",
}

var walCmd = cli.Command{
	Name:  "wal",
	Usage: "commit the spooled WAL files to the set named by --set, then delete them from the spool",
	Flags: []cli.Flag{
		spoolFlag,
		baseSetFlag,
		cli.DurationFlag{
			Name:  "window",
			Usage: "how far back point-in-time recovery reaches; WAL older than the oldest base backup inside it is let go",
			Value: 14 * 24 * time.Hour,
		},
	},
	Action: walAction,
}

func walAction(c *cli.Context) error {
	if c.String("spool") == "" || c.String("base-set") == "" || c.GlobalString("set") == "" {
		return cli.NewExitError("--set, --spool and --base-set are required", 2)
	}

	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)

	defer func() {
		index.Close()
		common.CloseStore(store)
	}()

	sessions, _ := store.(backup.SessionStore)
	spool := postgres.Spool{Dir: c.String("spool")}

	run := &postgres.WALBackup{
		Walker: &backup.Walker{
			Index:       index,
			Objects:     index,
			Sessions:    sessions,
			Set:         c.GlobalString("set"),
			AgentID:     common.AgentID(c),
			Key:         common.StoreKey(c, store),
			Root:        spool.Dir,
			Workers:     4,
			ReadRetries: 3,
		},
		Spool:   spool,
		BaseSet: c.String("base-set"),
		Window:  c.Duration("window"),
		Now:     time.Now,
	}

	result, err := run.Run(common.Context(c))
	if err != nil {
		return err
	}

	if result == nil {
		log.Print("The spool is empty")
		return nil
	}

	log.Printf("Commit %x: %d spooled files", result.Ref.Hash, result.Files)

	return nil
}
