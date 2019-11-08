package server

import (
	"log"
	"net"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/pack"

	"cloud.google.com/go/profiler"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/zipkin"
	zipkinHTTP "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/urfave/cli"
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
		cli.StringFlag{
			Name: "stackdriver-project",
		},
		cli.StringFlag{
			Name: "zipkin-url",
		},
	},
}

func enableStackdriver(projectID string) {
	sd, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID: projectID,
	})
	if err != nil {
		log.Fatalf("failed to create stackdriver exporter: %s", err)
	}

	err = profiler.Start(profiler.Config{
		ProjectID:      projectID,
		MutexProfiling: true,
		Service:        "goback",
	})
	if err != nil {
		log.Fatalf("failed setting up stackdriver profiler: %s", err)
	}

	view.RegisterExporter(sd)
	view.SetReportingPeriod(60 * time.Second)
	trace.RegisterExporter(sd)
}

func enableZipkin(url string) {
	reporter := zipkinHTTP.NewReporter(url)

	zk := zipkin.NewExporter(reporter, nil)

	trace.RegisterExporter(zk)
}

func serverAction(ctx *cli.Context) {
	s := common.GetObjectStore(ctx)
	listener, err := net.Listen("tcp", ctx.String("address"))
	if err != nil {
		log.Fatal(err)
	}

	err = view.Register(pack.DefaultViews...)
	if err != nil {
		log.Fatalf("failed to register pack storage views: %s", err)
	}

	err = view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		log.Fatalf("failed to register grpc server views: %s", err)
	}

	if projectID := ctx.String("stackdriver-project"); projectID != "" {
		enableStackdriver(projectID)
	}

	if url := ctx.String("zipkin-url"); url != "" {
		enableZipkin(url)
	}

	srv := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{
		IsPublicEndpoint: true,
		StartOptions: trace.StartOptions{
			SpanKind: trace.SpanKindServer,
			Sampler:  trace.ProbabilitySampler(1e-2),
		},
	}))

	proto.RegisterStoreServer(srv, storage.NewRemoteServer(s))

	log.Println("Listening on", listener.Addr().String())
	log.Fatal(srv.Serve(listener))
}
