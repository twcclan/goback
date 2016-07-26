package commit

import (
	"log"
	"os"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/codegangsta/cli"
)

func (c *commit) restore() {
	commits, err := c.index.CommitInfo(time.Now(), 1)
	if err != nil {
		log.Fatal(err)
	}

	if len(commits) == 1 {
		commit := commits[0]

		log.Printf("Walking tree %x for commit %d", commit.Tree.Sha1, commit.Timestamp)
		err := c.reader.WalkTree(commit.Tree, func(path string, info os.FileInfo, ref *proto.Ref) error {
			log.Printf("%s %9d %s %s", info.Mode(), info.Size(), info.ModTime().Format(time.ANSIC), path)
			return nil
		})

		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal("Commit not found")
	}
}

func restoreAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	index := common.GetIndex(c, store)
	log.Println(index.Open())

	s := &commit{
		index:  index,
		reader: backup.NewBackupReader(store),
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
}
