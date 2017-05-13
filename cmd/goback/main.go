package main

import (
	"log"
	"net/http"
	"os"
	"runtime"

	"github.com/codegangsta/cli"
	_ "github.com/joho/godotenv/autoload"

	"github.com/twcclan/goback/cmd/goback/commands/commit"
	"github.com/twcclan/goback/cmd/goback/commands/file"
	"github.com/twcclan/goback/cmd/goback/commands/fix"
	"github.com/twcclan/goback/cmd/goback/commands/object"

	_ "expvar"
	_ "net/http/pprof"
)

func webserver() {
	http.ListenAndServe(":8080", nil)
}

func main() {
	runtime.SetBlockProfileRate(1)
	go webserver()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	runtime.GOMAXPROCS(1)

	app := cli.NewApp()
	app.Name = "goback"
	app.Usage = "Take snapshots of your files and restore them."
	app.Commands = []cli.Command{
		commit.Command,
		file.Command,
		fix.Command,
		object.Command,
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
