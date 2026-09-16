package commit

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/twcclan/goback/backup"
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

// runHook runs a shell command with the process's stdio attached.
func runHook(name, command string) error {
	if command == "" {
		return nil
	}

	log.Printf("Running %s hook: %s", name, command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func newAction(c *cli.Context) {
	base := "."
	if c.Args().Present() {
		base = c.Args().First()
	}

	root, err := filepath.Abs(base)
	if err != nil {
		log.Fatal(err)
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	objects := backup.ObjectStore(index)

	var stats *statcache.Cache
	if dir := c.String("state-dir"); dir != "" {
		stats, err = statcache.Open(filepath.Join(dir, "stat"))
		if err != nil {
			log.Fatalf("opening stat cache: %v", err)
		}
		defer stats.Close()

		treeCache, err := badger.New(filepath.Join(dir, "objects"))
		if err != nil {
			log.Fatalf("opening object cache: %v", err)
		}
		defer treeCache.Close()

		objects = cache.New(treeCache, index)
	}

	agent := c.String("agent-id")
	if agent == "" {
		agent, _ = os.Hostname()
	}

	walker := &backup.Walker{
		Index:              index,
		Objects:            objects,
		Set:                c.GlobalString("set"),
		AgentID:            agent,
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

	err = runHook("pre", c.String("pre-hook"))
	if err != nil {
		log.Fatalf("pre hook failed: %v", err)
	}

	result, walkErr := walker.Run(context.Background())

	err = runHook("post", c.String("post-hook"))
	if err != nil {
		log.Printf("post hook failed: %v", err)
	}

	if walkErr != nil {
		log.Fatalf("%+v", walkErr)
	}

	log.Printf("Commit %x: %d files, %d reused, %d read, %d checkpoints", result.Ref.Hash, result.Files, result.Reused, result.Read, result.Checkpoints)

	if result.Torn > 0 || result.Unreadable > 0 || result.Skipped > 0 {
		log.Printf("%d files changed while being read, %d could not be read, %d irregular entries skipped", result.Torn, result.Unreadable, result.Skipped)
	}

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}

	if result.Dirty() {
		os.Exit(1)
	}
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
		cli.StringFlag{
			Name:  "agent-id",
			Usage: "identifier recorded in the commit; defaults to the hostname",
		},
		cli.StringFlag{
			Name:  "pre-hook",
			Usage: "shell command run before the walk, for example a save-off over RCON",
		},
		cli.StringFlag{
			Name:  "post-hook",
			Usage: "shell command run after the walk, even when it fails",
		},
	},
}
