package object

import (
	"context"
	"encoding/csv"
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

	writer := csv.NewWriter(out)
	err = writer.Write([]string{"ref", "timestamp", "set"})
	if err != nil {
		log.Fatal(err)
	}

	err = o.store.Walk(context.Background(), true, o.objectType, func(hdr *proto.ObjectHeader, obj *proto.Object) error {
		return writer.Write(
			[]string{
				fmt.Sprintf("%x", hdr.Ref.Sha1),
				fmt.Sprint(obj.GetCommit().GetTimestamp()),
				obj.GetCommit().GetBackupSet(),
			})
	})

	if err != nil {
		log.Fatal(err)
	}

	writer.Flush()
	err = writer.Error()
	if err != nil {
		log.Fatal(err)
	}

	err = out.Close()
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
}
