package server

import (
	"log"
	"net"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/pack"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"github.com/codegangsta/cli"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
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
		cli.BoolFlag{
			Name: "stackdriver",
		},
	},
}

func enableStackdriver() {
	sd, err := stackdriver.NewExporter(stackdriver.Options{})

	if err != nil {
		log.Fatalf("failed to create stackdriver exporter: %s", err)
	}

	view.RegisterExporter(sd)
	view.SetReportingPeriod(60 * time.Second)
	trace.RegisterExporter(sd)

	err = view.Register(pack.DefaultViews...)
	if err != nil {
		log.Fatalf("failed to register pack storage views: %s", err)
	}

	err = view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		log.Fatalf("failed to register grpc server views: %s", err)
	}
}

func serverAction(ctx *cli.Context) {
	s := common.GetObjectStore(ctx)
	listener, err := net.Listen("tcp", ctx.String("address"))
	if err != nil {
		log.Fatal(err)
	}

	if ctx.Bool("stackdriver") {
		enableStackdriver()
	}

	srv := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{
		IsPublicEndpoint: true,
	}))

	proto.RegisterStoreServer(srv, storage.NewRemoteServer(s))

	log.Println("Listening on", listener.Addr().String())
	log.Fatal(srv.Serve(listener))
}
