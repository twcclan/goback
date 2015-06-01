package main

import (
	"os"
	"runtime"

	"github.com/codegangsta/cli"
	"github.com/twcclan/goback/cmd/goback/commands"
)

func init() {

	/*
		metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)
		metrics.DefaultRegistry.Unregister("runtime.MemStats.BuckHashSys")
		metrics.DefaultRegistry.Unregister("runtime.MemStats.DebugGC")
		metrics.DefaultRegistry.Unregister("runtime.MemStats.EnableGC")
		metrics.DefaultRegistry.Unregister("runtime.MemStats.PauseNs")

		go metrics.CaptureRuntimeMemStats(metrics.DefaultRegistry, time.Millisecond*500)


			influxConfig := &influxdb.Config{
				Host:     "localhost:8086",
				Database: "goback",
				Username: "root",
				Password: "root",
			}

			go influxdb.Influxdb(metrics.DefaultRegistry, time.Second, influxConfig)
	*/
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "goback"
	app.Usage = "Take snapshots of your files and restore them."
	app.Commands = []cli.Command{
		commands.Snapshot,
		commands.Fetch,
	}

	app.Run(os.Args)

	//metrics.WriteOnce(metrics.DefaultRegistry, os.Stdout)
}
