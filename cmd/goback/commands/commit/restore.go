package commit

import (
	"context"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

type restoredDir struct {
	path    string
	modTime time.Time
}

func (c *commit) restore() error {
	commits, err := c.index.CommitInfo(context.Background(), c.set, c.when, 1)
	if err != nil {
		return err
	}

	if len(commits) != 1 {
		return errors.New("Commit not found")
	}

	commit := commits[0]
	log.Printf("Restoring commit %x from %v", proto.NewObject(commit).Ref().Hash, commit.Timestamp)
	tree := commit.Tree

	if c.from != "" {
		tree, err = c.reader.GetTree(context.Background(), tree, strings.Split(c.from, "/"))
		if err != nil {
			return err
		}
	}

	restored := map[string]bool{}
	var dirs []restoredDir

	err = c.reader.WalkTree(context.Background(), tree, func(path string, info os.FileInfo, ref *proto.Ref) error {
		path = filepath.Join(c.base, path)
		restored[path] = true
		log.Printf("Restoring %s", path)

		if info.IsDir() {
			// directory times are set after the subtree is written, or the children would clobber them
			dirs = append(dirs, restoredDir{path: path, modTime: info.ModTime()})

			return os.MkdirAll(path, info.Mode())
		}

		if stat, ok := info.Sys().(*proto.FileInfo); ok && stat.Type == proto.NodeType_NODE_SYMLINK {
			return restoreSymlink(path, stat.LinkTarget)
		}

		return c.restoreFile(path, info, ref)
	})
	if err != nil {
		return err
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		err = os.Chtimes(dirs[i].path, time.Now(), dirs[i].modTime)
		if err != nil {
			return err
		}
	}

	if c.delete {
		return removeUnrestored(c.base, restored)
	}

	return nil
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

// restoreFile writes the file next to its destination and renames it into
// place, so an interrupted restore never leaves a truncated file.
func (c *commit) restoreFile(path string, info os.FileInfo, ref *proto.Ref) error {
	reader, err := c.reader.ReadFile(context.Background(), ref)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".goback-*")
	if err != nil {
		return err
	}
	tmp := file.Name()

	_, err = io.Copy(file, reader)
	if err != nil {
		file.Close()
		os.Remove(tmp)
		return errors.Wrapf(err, "Restoring file: %s", path)
	}

	err = file.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	err = os.Chtimes(tmp, time.Now(), info.ModTime())
	if err != nil {
		os.Remove(tmp)
		return err
	}

	err = os.Chmod(tmp, info.Mode())
	if err != nil {
		os.Remove(tmp)
		return err
	}

	err = os.Rename(tmp, path)
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return nil
}

// removeUnrestored deletes everything under base that the restore did not
// write, deepest entries first.
func removeUnrestored(base string, restored map[string]bool) error {
	var stale []string

	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == base || restored[path] {
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
		log.Printf("Removing %s", path)

		err = os.RemoveAll(path)
		if err != nil {
			return err
		}
	}

	return nil
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

	if err := os.MkdirAll(dst, 0775); err != nil {
		log.Fatal(err)
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	s := &commit{
		index:  index,
		base:   filepath.Clean(dst),
		when:   when,
		from:   c.String("from"),
		delete: c.Bool("delete"),
		reader: backup.NewBackupReader(store),
		set:    c.GlobalString("set"),
	}

	err = s.restore()
	if err != nil {
		log.Fatal(err)
	}

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}

var restoreCmd = cli.Command{
	Name:        "restore",
	Description: "Restore all files from a given commit into a directory, keeping files the commit does not contain",
	Action:      restoreAction,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "from",
			Value: "",
		},
		cli.BoolFlag{
			Name:  "delete",
			Usage: "remove files under the target that the commit does not contain",
		},
	},
}
