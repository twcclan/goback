package main

import (
	"log"
	"os"
	"runtime"

	"github.com/twcclan/goback/cmd/goback/commands"
	"github.com/twcclan/goback/cmd/goback/commands/snapshot"

	"github.com/codegangsta/cli"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "goback"
	app.Usage = "Take snapshots of your files and restore them."
	app.Commands = []cli.Command{
		snapshot.Command,
		commands.Fetch,
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "storage",
			Value: "storage",
		},
	}

	app.Run(os.Args)
}
