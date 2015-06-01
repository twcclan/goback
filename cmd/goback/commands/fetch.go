package commands

import (
	"log"
	"time"

	"github.com/twcclan/goback/backup"

	"github.com/codegangsta/cli"
)

var Fetch = cli.Command{
	Name:   "fetch",
	Action: fetchCmd,
}

type fetch struct {
	store backup.ChunkStore
	index backup.Index
}

func (f *fetch) do() error {

	return nil
}

func fetchCmd(c *cli.Context) {
	src := c.Args().Get(0)
	//dst := c.Args().Get(1)
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

	idx := getIndex(c)
	store := getChunkStore(c)

	if err := idx.Open(); err != nil {
		log.Fatal(err)
	}

	ref, info, err := idx.Stat(src, when)
	if err != nil {
		log.Fatal(err)
	}

	if ref != nil {
		log.Printf("%s (%d): %x", info.Name, info.Timestamp, ref.Sum)
	} else {
		log.Fatalf("File %s not found", src)
	}

	f := &fetch{
		store: store,
		index: idx,
	}

	if err := f.do(); err != nil {
		log.Fatal(err)
	}

	if err := idx.Close(); err != nil {
		log.Fatal(err)
	}
}
