package commit

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

type restoredDir struct {
	path    string
	modTime time.Time
}

func (c *commit) restore() error {
	commits, err := c.index.CommitInfo(c.ctx, c.set, c.when, 1)
	if err != nil {
		return err
	}

	if len(commits) != 1 {
		return errors.New("Commit not found")
	}

	commit := commits[0]
	ref := proto.NewObject(commit).Ref()
	log.Printf("Restoring commit %x from %v", ref.Hash, commit.Timestamp)

	ctx, done, err := common.RestoreSession(c.ctx, c.store, c.set, ref)
	if err != nil {
		return err
	}
	defer done()
	c.ctx = ctx

	tree := commit.Tree
	var parent []byte

	if c.from != "" {
		tree, parent, err = c.reader.GetTree(c.ctx, tree, strings.Split(c.from, "/"))
		if err != nil {
			return err
		}
	}

	restored := map[string]bool{}
	var dirs []restoredDir
	var unrestored int

	err = c.reader.WalkTree(c.ctx, tree, parent, func(path string, info os.FileInfo, ref *proto.Ref) error {
		path = filepath.Join(c.base, path)
		restored[path] = true

		if info.IsDir() {
			// directory times are set after the subtree is written, or the children would clobber them
			dirs = append(dirs, restoredDir{path: path, modTime: info.ModTime()})

			if c.restorer.DryRun {
				return nil
			}

			return restoreDir(path, info.Mode())
		}

		stat, _ := info.Sys().(*proto.FileInfo)
		if stat == nil {
			return errors.Errorf("no stat for %s", path)
		}

		if stat.Type == proto.NodeType_NODE_SYMLINK {
			if c.restorer.DryRun {
				return nil
			}

			return restoreSymlink(path, string(stat.LinkTarget))
		}

		outcome, err := c.restorer.RestoreFile(c.ctx, path, stat, ref)
		if err != nil {
			// a salvage restores what it can and reports the rest
			if !c.restorer.Salvage {
				return err
			}

			unrestored++
			log.Printf("cannot restore %s: %v", path, err)

			return nil
		}

		if outcome != backup.OutcomeUnchanged && outcome != backup.OutcomeSkipped {
			log.Printf("%s: %s", outcome, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if c.delete {
		err = removeUnrestored(c.base, restored, c.restorer.DryRun)
		if err != nil {
			return err
		}
	}

	if !c.restorer.DryRun {
		for i := len(dirs) - 1; i >= 0; i-- {
			err = os.Chtimes(dirs[i].path, time.Now(), dirs[i].modTime)
			if err != nil {
				return err
			}
		}
	}

	logStats(c.restorer.Stats())

	if unrestored > 0 {
		log.Printf("%d files could not be restored at all", unrestored)
	}

	return nil
}

// restoreDir makes path the recorded directory: whatever else is there,
// a file or a symlink the restore must not write through, is removed.
func restoreDir(path string, mode os.FileMode) error {
	if existing, err := os.Lstat(path); err == nil && !existing.IsDir() {
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	err := os.MkdirAll(path, mode)
	if err != nil {
		return err
	}

	return os.Chmod(path, mode.Perm())
}

func logStats(stats backup.RestoreStats) {
	log.Printf("%d files: %d written, %d unchanged, %d skipped by stat; bytes from destination %s, seeds %s, cache %s, store %s",
		stats.Files, stats.Written, stats.Unchanged, stats.Skipped,
		humanize.Bytes(uint64(stats.BytesFromDestination)), humanize.Bytes(uint64(stats.BytesFromSeeds)),
		humanize.Bytes(uint64(stats.BytesFromCache)), humanize.Bytes(uint64(stats.BytesFromStore)))

	if stats.Salvaged > 0 {
		log.Printf("%d files salvaged with %s missing", stats.Salvaged, humanize.Bytes(uint64(stats.MissingBytes)))
	}
}

// restoreSymlink recreates a link as recorded and never follows it.
func restoreSymlink(path, target string) error {
	if existing, err := os.Readlink(path); err == nil && existing == target {
		return nil
	}

	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(target, path)
}

// removeUnrestored deletes everything under base that the restore did not
// write, deepest entries first.
func removeUnrestored(base string, restored map[string]bool, dryRun bool) error {
	folded := make(map[string]string, len(restored))
	for path := range restored {
		folded[strings.ToLower(path)] = path
	}

	// kept reports whether the restore wrote path under any spelling the
	// filesystem treats as the same name
	kept := func(path string) bool {
		if restored[path] {
			return true
		}

		spelled, ok := folded[strings.ToLower(path)]
		if !ok {
			return false
		}

		found, err := os.Lstat(path)
		if err != nil {
			return false
		}

		written, err := os.Lstat(spelled)
		if err != nil {
			return false
		}

		return os.SameFile(found, written)
	}

	var stale []string

	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == base || kept(path) {
			return nil
		}

		stale = append(stale, path)

		if d.IsDir() {
			return filepath.SkipDir
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, path := range stale {
		if dryRun {
			log.Printf("would remove: %s", path)
			continue
		}

		log.Printf("Removing %s", path)

		err = os.RemoveAll(path)
		if err != nil {
			return err
		}
	}

	return nil
}

func overwriteMode(name string) backup.OverwriteMode {
	switch name {
	case "always":
		return backup.OverwriteAlways
	case "if-changed":
		return backup.OverwriteIfChanged
	}

	log.Fatalf("unknown --overwrite mode %q: use always or if-changed", name)

	return backup.OverwriteAlways
}

func restoreAction(c *cli.Context) {
	dst := c.Args().Get(0)
	age := c.Args().Get(1)

	if age == "" {
		age = "0"
	}

	d, err := time.ParseDuration(age)
	if err != nil {
		log.Fatalf("Failed parsing <age> parameter: %v", err)
	}

	when := time.Now().Add(-d)
	base := filepath.Clean(dst)

	if !c.Bool("force") {
		held, err := backup.LiveMarkers(base)
		if err != nil {
			log.Fatal(err)
		}

		if len(held) > 0 {
			log.Fatalf("%s looks live: %s is held by another process; stop the server or pass --force", base, held[0])
		}
	}

	if !c.Bool("dry-run") {
		if err := os.MkdirAll(base, 0775); err != nil {
			log.Fatal(err)
		}
	}

	store := common.GetObjectStore(c)
	index := common.OpenIndex(c, store)
	key := common.StoreKey(c)

	restorer := &backup.Restorer{
		Store:     store,
		Key:       key,
		Cache:     common.BlobCache(c, key),
		Workers:   c.Int("workers"),
		Overwrite: overwriteMode(c.String("overwrite")),
		Verify:    c.Bool("verify"),
		DryRun:    c.Bool("dry-run"),
	}

	common.Salvage(c, restorer)

	if seeds := c.StringSlice("seed"); len(seeds) > 0 {
		restorer.Seeds = backup.NewSeedMap(key)
		for _, seed := range seeds {
			if err := restorer.Seeds.Add(seed); err != nil {
				log.Fatalf("indexing seed %s: %v", seed, err)
			}
		}

		log.Printf("Indexed %d chunks from %d seeds", restorer.Seeds.Len(), len(seeds))
	}

	s := &commit{
		ctx:      common.Context(c),
		index:    index,
		base:     base,
		when:     when,
		from:     c.String("from"),
		delete:   c.Bool("delete"),
		reader:   backup.NewBackupReader(store).WithKey(key),
		restorer: restorer,
		store:    store,
		set:      c.GlobalString("set"),
	}

	err = s.restore()
	if err != nil {
		log.Fatal(err)
	}

	common.SweepBlobCache(restorer.Cache)

	index.Close()

	common.CloseStore(store)
}

var restoreFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "overwrite",
		Usage: "always: hash every part of an existing file and rewrite what differs; if-changed: trust a size and mtime match",
		Value: "always",
	},
	cli.BoolFlag{
		Name:  "verify",
		Usage: "rehash every written file before it replaces the destination",
	},
	cli.BoolFlag{
		Name:  "dry-run",
		Usage: "report what would change without writing",
	},
	cli.StringSliceFlag{
		Name:  "seed",
		Usage: "file or directory whose chunks may be used instead of downloading; repeatable",
		Value: new(cli.StringSlice),
	},
	cli.IntFlag{
		Name:  "workers",
		Usage: "parts fetched at once",
		Value: 32,
	},
	common.SalvageFlag,
}

var restoreCmd = cli.Command{
	Name:        "restore",
	Description: "Restore a commit into a directory in place, downloading only the parts the directory does not already hold",
	Action:      restoreAction,
	Flags: append([]cli.Flag{
		cli.StringFlag{
			Name:  "from",
			Value: "",
		},
		cli.BoolFlag{
			Name:  "delete",
			Usage: "remove files under the target that the commit does not contain",
		},
		cli.BoolFlag{
			Name:  "force",
			Usage: "restore even when a session.lock under the target is held by a running server",
		},
	}, restoreFlags...),
}
