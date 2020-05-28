package commit

import (
	"context"
	"io"
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

func (c *commit) restore() {
	commits, err := c.index.CommitInfo(context.Background(), c.set, c.when, 1)
	if err != nil {
		log.Fatal(err)
	}

	if len(commits) == 1 {
		commit := commits[0]
		log.Printf("Restoring commit %x from %v", proto.NewObject(&commit).Ref().Sha1, commit.Timestamp)
		tree := commit.Tree

		if c.from != "" {
			tree, err = c.reader.GetTree(context.Background(), tree, strings.Split(c.from, "/"))
			if err != nil {
				log.Fatal(err)
			}
		}

		walkErr := c.reader.WalkTree(context.Background(), tree, func(path string, info os.FileInfo, ref *proto.Ref) error {
			path = filepath.Join(c.base, path)
			log.Printf("Restoring %s", path)

			if info.IsDir() {
				innerErr := os.Mkdir(path, info.Mode())

				if innerErr != nil {
					return innerErr
				}
			} else {
				reader, innerErr := c.reader.ReadFile(context.Background(), ref)
				if innerErr != nil {
					return innerErr
				}

				file, innerErr := os.Create(path)
				if innerErr != nil {
					return innerErr
				}
				// leave this here in case we return out early
				defer file.Close()

				_, innerErr = io.Copy(file, reader)
				if innerErr != nil {
					return errors.Wrapf(innerErr, "Restoring file: %s", path)
				}

				// close file here so chtimes works
				innerErr = file.Close()
				if innerErr != nil {
					return innerErr
				}
			}

			err = os.Chtimes(path, time.Now(), info.ModTime())
			if err != nil {
				return err
			}

			err = os.Chmod(path, info.Mode())
			if err != nil {
				return err
			}

			return nil
		})

		if walkErr != nil {
			log.Fatal(walkErr)
		}
	} else {
		log.Fatal("Commit not found")
	}
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
		base:   dst,
		when:   when,
		from:   c.String("from"),
		reader: backup.NewBackupReader(store),
		set:    c.GlobalString("set"),
	}

	s.restore()

	index.Close()

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}
}

var restoreCmd = cli.Command{
	Name:        "restore",
	Description: "Restore all files from a given commit",
	Action:      restoreAction,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "from",
			Value: "",
		},
	},
}
