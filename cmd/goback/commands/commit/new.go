package commit

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/hooks"
	"github.com/twcclan/goback/backup/statcache"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/badger"
	"github.com/twcclan/goback/storage/cache"

	"github.com/bmatcuk/doublestar"
	"github.com/urfave/cli"
)

// includeFilter applies the include and exclude patterns to a path relative
// to the backup root, written with a leading slash.
func includeFilter(includes, excludes []string) func(string) bool {
	match := func(patterns []string, name string) bool {
		for _, pat := range patterns {
			ok, err := doublestar.Match(pat, name)
			if err != nil {
				log.Printf("Malformed pattern: \"%s\" %v", pat, err)
			}

			if ok {
				return true
			}
		}

		return false
	}

	return func(rel string) bool {
		name := "/" + rel

		if match(includes, name) {
			return true
		}

		return !match(excludes, name)
	}
}

// errDirty marks a run that committed but recorded torn or unreadable files.
var errDirty = errors.New("some files were torn or unreadable")

func newAction(c *cli.Context) {
	err := runNew(c)
	if errors.Is(err, errDirty) {
		os.Exit(1)
	}

	if err != nil {
		log.Fatalf("%+v", err)
	}
}

func runNew(c *cli.Context) error {
	base := "."
	if c.Args().Present() {
		base = c.Args().First()
	}

	root, err := filepath.Abs(base)
	if err != nil {
		return err
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)

	defer func() {
		index.Close()

		if cl, ok := store.(common.Closer); ok {
			log.Println(cl.Close())
		}
	}()

	objects := backup.ObjectStore(index)

	var stats *statcache.Cache
	if dir := c.String("state-dir"); dir != "" {
		stats, err = statcache.Open(filepath.Join(dir, "stat"))
		if err != nil {
			return errors.Join(errors.New("opening stat cache"), err)
		}
		defer stats.Close()

		treeCache, err := badger.New(filepath.Join(dir, "objects"))
		if err != nil {
			return errors.Join(errors.New("opening object cache"), err)
		}
		defer treeCache.Close()

		objects = cache.New(treeCache, index)
	}

	sessions, _ := store.(backup.SessionStore)

	walker := &backup.Walker{
		Index:              index,
		Objects:            objects,
		Sessions:           sessions,
		Set:                c.GlobalString("set"),
		AgentID:            common.AgentID(c),
		Metadata:           common.Metadata(c),
		Key:                common.StoreKey(c),
		Root:               root,
		Include:            includeFilter(c.StringSlice("include"), c.StringSlice("exclude")),
		Workers:            c.Int("workers"),
		ForceHashPercent:   c.Int("force-hash"),
		CheckpointInterval: c.Duration("checkpoint-interval"),
		ReadRetries:        c.Int("read-retries"),
		PrefetchDepth:      2,
	}

	if stats != nil {
		walker.Cache = stats
	}

	if c.GlobalBoolT("blob-cache-on-backup") {
		walker.BlobCache = common.BlobCache(c, walker.Key)
	}

	ctx := common.Context(c)
	if d := c.Duration("max-duration"); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	runner := &hooks.Runner{
		Pre:     c.String("pre-hook"),
		Post:    c.String("post-hook"),
		Timeout: c.Duration("hook-timeout"),
		Logf:    log.Printf,
	}

	preOut, err := runner.RunPre(ctx)
	if err != nil {
		if !c.Bool("hook-optional") {
			// the hook may have quiesced the application before failing
			if postErr := runner.RunPost(ctx); postErr != nil {
				log.Println(postErr)
			}

			return err
		}

		log.Printf("%v; continuing because --hook-optional is set", err)
	}

	walker.Quiesced = runner.Pre != "" && err == nil

	if dir := hooks.Redirect(preOut); dir != "" {
		walker.Root, err = filepath.Abs(dir)
		if err != nil {
			return err
		}

		log.Printf("Pre hook redirected the walk to %s", walker.Root)
	}

	result, walkErr := walker.Run(ctx)

	if err := runner.RunPost(ctx); err != nil {
		log.Println(err)
	}

	if walkErr != nil {
		return walkErr
	}

	log.Printf("Commit %x: %d files, %d reused, %d read, %d checkpoints", result.Ref.Hash, result.Files, result.Reused, result.Read, result.Checkpoints)

	common.SweepBlobCache(walker.BlobCache)

	if result.Torn > 0 || result.Unreadable > 0 || result.Skipped > 0 {
		log.Printf("%d files changed while being read, %d could not be read, %d irregular entries skipped", result.Torn, result.Unreadable, result.Skipped)
	}

	if result.Dirty() {
		return errDirty
	}

	return nil
}

var newCmd = cli.Command{
	Name:        "new",
	Description: "Create a new commit by diffing the directory against the set's latest commit",
	Action:      newAction,
	Flags: []cli.Flag{
		cli.StringSliceFlag{
			Name:  "include, i",
			Value: new(cli.StringSlice),
		},
		cli.StringSliceFlag{
			Name:  "exclude, e",
			Value: new(cli.StringSlice),
		},
		cli.IntFlag{
			Name:  "workers, w",
			Value: runtime.NumCPU(),
		},
		cli.IntFlag{
			Name:  "force-hash",
			Usage: "percent of unchanged files to re-hash locally as a check on the change test",
		},
		cli.DurationFlag{
			Name:  "checkpoint-interval",
			Usage: "write a partial commit this often so an interrupted run keeps its progress; 0 disables",
			Value: 30 * time.Minute,
		},
		cli.IntFlag{
			Name:  "read-retries",
			Usage: "how often to re-read a file that changes while it is being read",
			Value: 3,
		},
		cli.StringFlag{
			Name:  "state-dir",
			Usage: "per-machine directory for the stat cache and tree cache; optional",
		},
		common.MetaFlag,
		cli.StringFlag{
			Name:  "pre-hook",
			Usage: "shell command run before the walk, for example one that pauses the application's writes; see contrib/hooks",
		},
		cli.StringFlag{
			Name:  "post-hook",
			Usage: "shell command run after the walk on every exit the agent controls: success, failure, Ctrl-C and deadlines",
		},
		cli.DurationFlag{
			Name:  "hook-timeout",
			Usage: "deadline for each hook",
			Value: 5 * time.Minute,
		},
		cli.BoolFlag{
			Name:  "hook-optional",
			Usage: "back up anyway when the pre hook fails",
		},
		cli.DurationFlag{
			Name:  "max-duration",
			Usage: "cancel the walk after this long, still running the post hook; 0 disables",
		},
	},
}
