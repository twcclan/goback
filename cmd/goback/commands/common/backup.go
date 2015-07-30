package common

import (
	"github.com/codegangsta/cli"
	"github.com/twcclan/goback/backup"
)

func GetChunkStore(c *cli.Context) backup.ChunkStore {
	return backup.NewSimpleChunkStore("storage")
}

func GetIndex(c *cli.Context) backup.Index {
	return backup.NewSqliteIndex("storage")
}
