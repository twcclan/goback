package file

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/codegangsta/cli"
)

func (f *file) restore() error {
	info, err := f.reader.Stat(f.src)
	if err != nil {
		return err
	}

	log.Printf("name: %s, size: %d, mod: %s", info.Name(), info.Size(), info.ModTime())

	outFile, err := ioutil.TempFile(filepath.Dir(f.dst), filepath.Base(f.dst))
	if err != nil {
		return err
	}
	defer outFile.Close()

	inFile, err := f.reader.Open(f.src)
	if err != nil {
		return err
	}

	_, err = io.Copy(outFile, inFile)
	if err != nil {
		return err
	}

	outFile.Close()

	err = os.Chtimes(outFile.Name(), time.Now(), info.ModTime())
	if err != nil {
		return err
	}

	err = os.Chmod(outFile.Name(), info.Mode())
	if err != nil {
		return err
	}

	err = os.Rename(outFile.Name(), f.dst)
	if err != nil {
		return err
	}

	return nil
}

func restoreAction(c *cli.Context) {
	src := c.Args().Get(0)
	dst := c.Args().Get(1)
	age := c.Args().Get(2)
	if age == "" {
		age = "0"
	}

	d, err := time.ParseDuration(age)
	if err != nil {
		log.Fatalf("Failed parsing <age> parameter: %v", err)
	}

	when := time.Now().Add(-d)

	log.Printf("Searching file as old as %s", when)

	idx := common.GetIndex(c)
	store := common.GetChunkStore(c)

	if err := idx.Open(); err != nil {
		log.Fatal(err)
	}

	f := &file{
		reader: backup.NewBackupReader(idx, store, when),
		src:    src,
		dst:    dst,
	}

	if err := f.restore(); err != nil {
		log.Fatal(err)
	}

	if err := idx.Close(); err != nil {
		log.Fatal(err)
	}
}

var restoreCmd = cli.Command{
	Name:        "restore",
	Description: "Restore a file",
	Action:      restoreAction,
}
