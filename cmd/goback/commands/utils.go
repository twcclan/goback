package commands

import (
	"github.com/twcclan/goback/backup"

	"github.com/codegangsta/cli"
)

func getChunkStore(c *cli.Context) backup.ChunkStore {
	return backup.NewNopStorage()
}

func getIndex(c *cli.Context) backup.Index {
	return backup.NewSqliteIndex("storage")
}
