package object

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/codegangsta/cli"
)

func (o *object) list() {
	out, err := os.Create("objects.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	err = o.store.Walk(context.Background(), false, o.objectType, func(hdr *proto.ObjectHeader, obj *proto.Object) error {
		if hdr.Size > 1<<20 {
			fmt.Fprintf(out, "%x,%d\n", hdr.Ref.Sha1, hdr.Size)
		}
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

}

func listAction(c *cli.Context) {
	store := common.GetObjectStore(c)
	objectType := proto.ObjectType(proto.ObjectType_value[strings.ToUpper(c.String("type"))])

	o := &object{
		store:      store,
		objectType: objectType,
	}

	o.list()
}

var listCmd = cli.Command{
	Name:        "list",
	Description: "List all objects",
	Action:      listAction,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name: "type",
		},
	},
}
