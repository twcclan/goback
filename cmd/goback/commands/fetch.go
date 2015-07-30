package commands

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

var Fetch = cli.Command{
	Name:   "fetch",
	Action: fetchCmd,
}

type fetch struct {
	store backup.ChunkStore
	index backup.Index
	src   string
	dst   string
	when  time.Time
}

func (f *fetch) do() error {
	reader := backup.NewBackupReader(f.index, f.store, f.when)
	info, err := reader.Stat(f.src)
	if err != nil {
		return err
	}

	log.Printf("name: %s, size: %d, mod: %s", info.Name(), info.Size(), info.ModTime())

	outFile, err := ioutil.TempFile(filepath.Dir(f.dst), filepath.Base(f.dst))
	if err != nil {
		return err
	}
	defer outFile.Close()

	inFile, err := reader.Open(f.src)
	if err != nil {
		return err
	}

	_, err = io.Copy(outFile, inFile)
	if err != nil {
		return err
	}

	outFile.Close()

	err = os.Rename(outFile.Name(), f.dst)
	if err != nil {
		return err
	}

	return nil
}

func fetchCmd(c *cli.Context) {
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

	f := &fetch{
		store: store,
		index: idx,
		src:   src,
		dst:   dst,
		when:  when,
	}

	if err := f.do(); err != nil {
		log.Fatal(err)
	}

	if err := idx.Close(); err != nil {
		log.Fatal(err)
	}
}
