package main

import (
	_ "expvar"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/trace"
	"time"

	"github.com/twcclan/goback/cmd/goback/commands/commit"
	"github.com/twcclan/goback/cmd/goback/commands/file"
	"github.com/twcclan/goback/cmd/goback/commands/fix"
	"github.com/twcclan/goback/cmd/goback/commands/object"
	"github.com/twcclan/goback/cmd/goback/commands/server"
	"github.com/twcclan/goback/storage/pack"

	"github.com/codegangsta/cli"
	_ "github.com/joho/godotenv/autoload"
	prom "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
)

func webserver() {
	http.ListenAndServe(":8080", nil)
}

func tracer() {
	f, err := os.Create("trace.out")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	trace.Start(f)

	time.Sleep(15 * time.Second)
	trace.Stop()
}

func main() {
	exp, _ := prometheus.NewExporter(prometheus.Options{Registry: prom.NewRegistry()})
	view.RegisterExporter(exp)
	view.Register(pack.DefaultViews...)

	http.Handle("/metrics", exp)

	go webserver()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	runtime.GOMAXPROCS(runtime.NumCPU() + 1)

	app := cli.NewApp()
	app.Name = "goback"
	app.Usage = "Take snapshots of your files and restore them."
	app.Commands = []cli.Command{
		commit.Command,
		file.Command,
		fix.Command,
		object.Command,
		server.Command,
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "storage",
			Value: "storage",
		},
		cli.StringFlag{
			Name:  "index",
			Value: "index",
		},
	}

	app.Run(os.Args)
}
