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
	"github.com/twcclan/goback/cmd/goback/commands/gc"
	"github.com/twcclan/goback/cmd/goback/commands/key"
	"github.com/twcclan/goback/cmd/goback/commands/maintain"
	"github.com/twcclan/goback/cmd/goback/commands/object"
	"github.com/twcclan/goback/cmd/goback/commands/pin"
	"github.com/twcclan/goback/cmd/goback/commands/scrub"
	"github.com/twcclan/goback/cmd/goback/commands/server"
	"github.com/twcclan/goback/cmd/goback/commands/set"
	"github.com/twcclan/goback/telemetry"

	_ "github.com/joho/godotenv/autoload"
	"github.com/urfave/cli"
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
	metrics, err := telemetry.Metrics()
	if err != nil {
		log.Fatalf("failed setting up metrics: %s", err)
	}

	http.Handle("/metrics", metrics)

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
		gc.Command,
		key.Command,
		maintain.Command,
		object.Command,
		pin.Command,
		scrub.Command,
		server.Command,
		set.Command,
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
		cli.StringFlag{
			Name: "set, s",
		},
		cli.StringFlag{
			Name:  "agent-id",
			Usage: "identifier recorded in commits and presented to a goback:// store server; defaults to the hostname",
		},
		cli.StringFlag{
			Name:  "secret",
			Usage: "the secret shared with a goback:// store server",
		},
		cli.StringFlag{
			Name:  "ca-cert",
			Usage: "PEM certificate authority a goback:// store server must present; the system roots are trusted without it",
		},
		cli.StringFlag{
			Name:  "store-key",
			Usage: "store key file; names and contents are encrypted with it before upload",
		},
		cli.StringFlag{
			Name:  "at-rest-key",
			Usage: "key file a pack:// or gcs:// store seals its archives with; empty stores objects as received",
		},
		cli.BoolFlag{
			Name:  "reset-index",
			Usage: "drop the local archive index and rebuild it from the archives",
		},
		cli.StringFlag{
			Name:  "blob-cache",
			Usage: "directory holding copies of stored blobs, consulted before downloading during a restore; empty disables it",
		},
		cli.StringFlag{
			Name:  "blob-cache-size",
			Usage: "size the blob cache is trimmed to after a backup or restore",
			Value: "4GB",
		},
		cli.BoolTFlag{
			Name:  "blob-cache-on-backup",
			Usage: "also put every uploaded blob into the blob cache, so a rollback to the last backup downloads nothing",
		},
	}

	app.Run(os.Args)
}
