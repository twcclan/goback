package common

import (
	"log"
	"os"

	"github.com/codegangsta/cli"
	"github.com/twcclan/goback/backup"
)

func getStorageLocation(c *cli.Context) string {
	loc := c.GlobalString("storage")

	err := os.MkdirAll(loc, os.FileMode(0700))
	if err != nil {
		if os.IsExist(err) {
			log.Fatal(err)
		}
	}

	return loc
}

func GetChunkStore(c *cli.Context) backup.ChunkStore {
	return backup.NewSimpleChunkStore(getStorageLocation(c))
}

func GetIndex(c *cli.Context) backup.Index {
	return backup.NewSqliteIndex(getStorageLocation(c))
}
