package scrub

import (
	"context"
	"log"
	"os"

	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/storage/pack"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

// Scrubber is implemented by stores that can rehash everything they hold.
type Scrubber interface {
	Scrub(ctx context.Context) (*pack.ScrubReport, error)
}

var Command = cli.Command{
	Name:        "scrub",
	Description: "Rehash every stored object and report corruption; exits non-zero if any object fails",
	Action:      scrubAction,
}

func scrubAction(c *cli.Context) {
	store := common.GetObjectStore(c)

	scrubber, ok := common.Unwrap(store).(Scrubber)
	if !ok {
		log.Fatalf("storage %s cannot be scrubbed", c.GlobalString("storage"))
	}

	report, err := scrubber.Scrub(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Scrubbed %d objects (%s) in %d archives, %d corrupt", report.Objects, humanize.Bytes(report.Bytes), report.Archives, len(report.Corrupt))

	if cl, ok := store.(common.Closer); ok {
		log.Println(cl.Close())
	}

	if len(report.Corrupt) > 0 {
		os.Exit(1)
	}
}
