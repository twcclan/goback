package server

import (
	"log"
	"net"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"

	"github.com/codegangsta/cli"
	"google.golang.org/grpc"
)

var Command = cli.Command{
	Action:      serverAction,
	Name:        "server",
	Description: "Run an object store server.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "address",
			Value: ":6060",
		},
	},
}

func serverAction(ctx *cli.Context) {
	s := common.GetObjectStore(ctx)
	listener, err := net.Listen("tcp", ctx.String("address"))
	if err != nil {
		log.Fatal(err)
	}

	srv := grpc.NewServer()

	proto.RegisterStoreServer(srv, storage.NewRemoteServer(s))

	log.Println("Listening on", listener.Addr().String())
	log.Fatal(srv.Serve(listener))
}
