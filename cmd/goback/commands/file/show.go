package file

import (
	"context"
	"log"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func (f *file) show() error {
	nodes, err := f.index.FileInfo(context.Background(), f.src, f.when, 10)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		log.Print("File not found in index")

		return nil
	}

	log.Println("Listing all versions for file:", nodes[0].Stat.Name)

	for _, node := range nodes {
		info := node.Stat
		log.Printf("dir: %v size: %d timestamp: %v", info.Tree, info.Size, time.Unix(info.Timestamp, 0))
	}

	return nil
}

func showAction(c *cli.Context) error {
	src := c.Args().Get(0)
	age := c.Args().Get(1)
	if age == "" {
		age = "0"
	}

	d, err := time.ParseDuration(age)
	if err != nil {
		return errors.Wrap(err, "Failed parsing <age> parameter: %v")
	}

	when := time.Now().Add(-d)

	store := common.GetObjectStore(c)
	idx := common.GetIndex(c, store)

	if err := idx.Open(); err != nil {
		return err
	}

	f := &file{
		src:   src,
		index: idx,
		when:  when,
	}

	if err := f.show(); err != nil {
		return err
	}

	if err := idx.Close(); err != nil {
		return err
	}

	return nil
}

var showCmd = cli.Command{
	Name:        "show",
	Description: "Show information about a file",
	Action:      showAction,
}
