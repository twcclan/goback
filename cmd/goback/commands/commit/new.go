package commit

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/bmatcuk/doublestar"
	"github.com/codegangsta/cli"
	"github.com/pkg/errors"
	"go4.org/syncutil"
	"golang.org/x/sync/errgroup"
)

func (c *commit) shouldInclude(fName string) bool {
	// check the patterns against paths relative to our base
	fName = strings.TrimPrefix(fName, c.base)

	// check whitelist first
	for _, pat := range c.includes {
		match, err := doublestar.Match(pat, fName)
		if err != nil {
			log.Printf("Malformed pattern: \"%s\" %v", pat, err)
		}

		if match {
			//log.Printf("Whitelisted file: %s", fName)
			return true
		}
	}

	// check blacklist
	for _, pat := range c.excludes {
		match, err := doublestar.Match(pat, fName)
		if err != nil {
			log.Printf("Malformed pattern: \"%s\" %v", pat, err)
		}

		if match {
			//log.Printf("Blacklisted file: %s", fName)
			return false
		}
	}

	// default to allowing
	return true
}

func (c *commit) read(file string) func(io.Writer) error {
	return func(writer io.Writer) error {
		reader, err := os.Open(file)
		if err != nil {
			return errors.Wrapf(err, "Failed opening %s for reading", file)
		}
		defer reader.Close()

		_, err = io.Copy(writer, reader)

		return errors.Wrapf(err, "Failed writing file %s", file)
	}
}

func (c *commit) descend(base string) func(backup.TreeWriter) error {
	return func(tree backup.TreeWriter) error {
		group := errgroup.Group{}

		files, err := ioutil.ReadDir(base)
		if err != nil {
			return errors.Wrapf(err, "Failed opening %s for listing", base)
		}

		for _, file := range files {
			info := file
			absPath := filepath.ToSlash(filepath.Join(base, info.Name()))

			if !c.shouldInclude(absPath) {
				continue
			}

			if info.IsDir() {
				// recurse into the sub folder
				tErr := tree.Tree(context.Background(), info, c.descend(absPath))
				if tErr != nil {
					return tErr
				}

				continue
			}

			// spawn a limited number of workers in parallel for file backups
			c.gate.Start()
			group.Go(func() error {
				defer c.gate.Done()

				if info.Mode()&os.ModeSymlink == 0 { // skip symlinks
					var nodes []proto.TreeNode
					nodes, err = c.index.FileInfo(context.Background(), strings.TrimPrefix(absPath, c.base+"/"), time.Now(), 1)
					if err != nil {
						return errors.Wrapf(err, "Failed checking index for info %s", absPath)
					}

					if len(nodes) > 0 && nodes[0].Stat.Timestamp == info.ModTime().Unix() {
						// apparently we have this file already
						tree.Node(&nodes[0])
					} else {
						// store the file
						return tree.File(context.Background(), info, c.read(absPath))
					}
				}

				return nil
			})
		}

		return group.Wait()
	}
}

func (c *commit) take() {
	err := c.descend(c.base)(c.backup)

	if err != nil {
		log.Printf("%+v", err)
		os.Exit(-1)
	}

	err = c.backup.Close(context.Background())
	if err != nil {
		log.Fatal(err)
	}
}

func newAction(c *cli.Context) {
	base := "."
	if c.Args().Present() {
		base = c.Args().First()
	}

	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	s := &commit{
		backup:   backup.NewBackupWriter(index, c.GlobalString("set")),
		base:     filepath.ToSlash(filepath.Clean(base)),
		index:    index,
		includes: c.StringSlice("include"),
		excludes: c.StringSlice("exclude"),
		gate:     syncutil.NewGate(c.Int("workers")),
	}

	s.take()

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}

var newCmd = cli.Command{
	Name:        "new",
	Description: "Create a new commit",
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
	},
}
