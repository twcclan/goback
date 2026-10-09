package postgres

import (
	"fmt"
	"log"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/backup/blobcache"
	"github.com/gobackio/goback/backup/postgres"
	"github.com/gobackio/goback/cmd/goback/commands/common"
	"github.com/gobackio/goback/storage/cache"

	"github.com/urfave/cli"
)

var baseSetFlag = cli.StringFlag{
	Name:  "base-set",
	Usage: "set the cluster's base backups go to",
}

// treeCacheSize bounds the commits and trees kept under --cache-dir; the
// latest commit's are read every run, so they are never the ones let go.
const treeCacheSize = 256 << 20

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
		common.CloseAll(store, index)
	}()

	sessions, _ := store.(backup.SessionStore)
	spool := postgres.Spool{Dir: c.String("spool")}

	objects := backup.ObjectStore(index)

	var trees *blobcache.Cache
	if dir := common.StoreCache(c, "backup"); dir != "" {
		var err error
		if trees, err = blobcache.Open(dir, "trees", treeCacheSize); err != nil {
			return fmt.Errorf("opening the tree cache: %w", err)
		}

		objects = cache.New(trees, index)
	}

	run := &postgres.WALBackup{
		Walker: &backup.Walker{
			Index:       index,
			Objects:     objects,
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

	if trees != nil {
		if _, err := trees.Sweep(); err != nil {
			log.Printf("sweeping the tree cache: %v", err)
		}
	}

	if result == nil {
		common.Result(struct {
			Files int64 `json:"files"`
		}{}, func() { log.Print("The spool is empty") })

		return nil
	}

	common.Result(common.View.Walk(result), func() { log.Printf("Commit %x: %d spooled files", result.Ref.Hash, result.Files) })

	return nil
}
