package object

import (
	"log"
	"strings"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/codegangsta/cli"
)

func (o *object) list() {
	err := o.store.Walk(o.objectType, func(obj *proto.Object) error {
		log.Printf("%x %s", obj.Ref().Sha1, obj.Type().String())
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
