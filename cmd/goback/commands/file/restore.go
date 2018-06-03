package file

import (
	"context"
	"errors"
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
	files, err := f.index.FileInfo(context.Background(), f.src, f.when, 1)
	if err != nil {
		return err
	}

	if len(files) != 1 {
		return errors.New("Couldn't find file")
	}

	info := files[0].Stat

	log.Printf("name: %s, size: %d, mod: %s", info.Name, info.Size, time.Unix(info.Timestamp, 0))

	outFile, err := ioutil.TempFile(filepath.Dir(f.dst), filepath.Base(f.dst))
	if err != nil {
		return err
	}
	defer outFile.Close()

	inFile, err := f.reader.ReadFile(context.Background(), files[0].Ref)
	if err != nil {
		return err
	}

	_, err = io.Copy(outFile, inFile)
	if err != nil {
		return err
	}

	outFile.Close()

	err = os.Chtimes(outFile.Name(), time.Now(), time.Unix(info.Timestamp, 0))
	if err != nil {
		return err
	}

	err = os.Chmod(outFile.Name(), os.FileMode(info.Mode))
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

	store := common.GetObjectStore(c)
	idx := common.GetIndex(c, store)

	if err := idx.Open(); err != nil {
		log.Fatal(err)
	}

	f := &file{
		reader: backup.NewBackupReader(store),
		src:    src,
		dst:    dst,
		when:   when,
		index:  idx,
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
