package object

import (
	"context"
	"fmt"
	"log"

	"github.com/twcclan/goback/backup"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/urfave/cli"
)

func (o *object) count() {
	// if our store provides counting functionality use that ...
	if counter, ok := o.store.(backup.Counter); ok {
		total, unique, err := counter.Count()
		if err == nil {
			log.Printf("Counted %d total and %d unique objects", total, unique)
			return
		}

		log.Println(fmt.Errorf("couldn't count objects, using fallback: %w", err))
	}

	// ... otherwise fall back to counting ourselves
	var count uint64

	err := o.store.Walk(context.Background(), false, proto.ObjectType_INVALID, func(p *proto.Object) error {
		count++

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Counted %d objects", count)
}

func countAction(c *cli.Context) {
	store := common.GetObjectStore(c)

	o := &object{
		store: store,
	}

	o.count()
}

var countCmd = cli.Command{
	Name:        "count",
	Description: "Count all objects",
	Action:      countAction,
}
