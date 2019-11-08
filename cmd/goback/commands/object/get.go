package object

import (
	"context"
	"encoding/hex"
	"io/ioutil"
	"log"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"

	"github.com/urfave/cli"
)

func (o *object) get() {
	obj, err := o.store.Get(context.Background(), o.ref)
	if err != nil {
		log.Fatal(err)
	}

	if obj == nil {
		log.Fatal("Object not found")
	}

	log.Printf("Found object %s", obj.Type())

	if obj.Type() == proto.ObjectType_FILE {
		log.Printf("%d parts", len(obj.GetFile().Parts))
	}

	err = ioutil.WriteFile(o.out, obj.Bytes(), 0666)
	if err != nil {
		log.Fatal(err)
	}
}

func getAction(c *cli.Context) {
	hash, err := hex.DecodeString(c.Args().Get(0))
	out := c.Args().Get(1)

	if err != nil {
		log.Fatal(err)
	}

	store := common.GetObjectStore(c)

	o := &object{
		store: store,
		ref:   &proto.Ref{Sha1: hash},
		out:   out,
	}

	o.get()
}

var getCmd = cli.Command{
	Name:        "get",
	Description: "Get a single object",
	Action:      getAction,
}
