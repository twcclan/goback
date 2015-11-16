package main

import (
	"log"
	"os"
	"runtime"

	"github.com/twcclan/goback/cmd/goback/commands/file"
	"github.com/twcclan/goback/cmd/goback/commands/snapshot"

	"github.com/codegangsta/cli"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "goback"
	app.Usage = "Take snapshots of your files and restore them."
	app.Commands = []cli.Command{
		snapshot.Command,
		file.Command,
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
