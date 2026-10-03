package file

import (
	"log"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func (f *file) list() error {
	lister, ok := f.index.(backup.DirLister)
	if !ok {
		return errors.Errorf("index %T lists no directories", f.index)
	}

	entries, err := lister.ReadDir(f.ctx, f.set, f.src, f.when)
	if err != nil {
		return err
	}

	views := make([]common.NodeView, len(entries))
	for i, entry := range entries {
		views[i] = common.ViewNode(entry)
	}

	if common.JSON() {
		common.Result(views, nil)

		return nil
	}

	if len(entries) == 0 {
		log.Print("Nothing there at that time")

		return nil
	}

	for _, entry := range entries {
		info := entry.Stat

		switch info.GetType() {
		case proto.NodeType_NODE_DIRECTORY:
			log.Printf("%s/", info.GetName())
		case proto.NodeType_NODE_SYMLINK:
			log.Printf("%s -> %s", info.GetName(), info.GetLinkTarget())
		default:
			log.Printf("%s size: %d timestamp: %v", info.GetName(), info.GetSize(), info.ModTime())
		}
	}

	return nil
}

func lsAction(c *cli.Context) error {
	age := c.Args().Get(1)
	if age == "" {
		age = "0"
	}

	d, err := time.ParseDuration(age)
	if err != nil {
		return errors.Wrap(err, "Failed parsing <age> parameter: %v")
	}

	store := common.GetObjectStore(c)
	idx := common.OpenIndex(c, store)

	f := &file{
		ctx:   common.Context(c),
		src:   c.Args().Get(0),
		index: idx,
		when:  time.Now().Add(-d),
		set:   c.GlobalString("set"),
	}

	if err := f.list(); err != nil {
		common.Fatal(err)
	}

	if err := idx.Close(); err != nil {
		common.Fatal(err)
	}

	return nil
}

var lsCmd = cli.Command{
	Name:        "ls",
	Usage:       "ls <dir> [age]",
	Description: "List a directory as the set held it, now or <age> ago",
	Action:      lsAction,
}
