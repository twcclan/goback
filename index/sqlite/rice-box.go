package sqlite

import (
	"time"

	"github.com/GeertJohan/go.rice/embedded"
)

func init() {

	// define files
	file2 := &embedded.EmbeddedFile{
		Filename:    "settings.sql",
		FileModTime: time.Unix(1590313900, 0),

		Content: string("PRAGMA journal_mode = MEMORY;\nPRAGMA synchronous = OFF;"),
	}
	file3 := &embedded.EmbeddedFile{
		Filename:    "tables.sql",
		FileModTime: time.Unix(1590313900, 0),

		Content: string("CREATE TABLE IF NOT EXISTS `objects`(\n\t`ref`\tBLOB NOT NULL,\n\t`type` INTEGER NOT NULL,\n\t`indexed` INTEGER NOT NULL,\n\tPRIMARY KEY(ref)\n) WITHOUT ROWID;\n\nCREATE TABLE IF NOT EXISTS `files`(\n\t`path` TEXT NOT NULL,\n\t`mode` INTEGER NOT NULL,\n\t--`user` TEXT NOT NULL,\n\t--`group` TEXT NOT NULL,\n\t`timestamp` INTEGER NOT NULL,\n\t`size` INTEGER NOT NULL,\n\t'ref' BLOB NOT NULL,\n\tPRIMARY KEY(`path`, `timestamp`),\n\tFOREIGN KEY(ref) REFERENCES objects(ref)\n\t--FOREIGN KEY(data) REFERENCES chunks(sum)\n) WITHOUT ROWID;\n\nCREATE TABLE IF NOT EXISTS `commits`(\n\t`timestamp` INTEGER NOT NULL,\n\t'tree' BLOB NOT NULL,\n\tPRIMARY KEY(timestamp),\n\tFOREIGN KEY(tree) REFERENCES objects(ref)\n) WITHOUT ROWID;\n"),
	}

	// define dirs
	dir1 := &embedded.EmbeddedDir{
		Filename:   "",
		DirModTime: time.Unix(1590313900, 0),
		ChildFiles: []*embedded.EmbeddedFile{
			file2, // "settings.sql"
			file3, // "tables.sql"

		},
	}

	// link ChildDirs
	dir1.ChildDirs = []*embedded.EmbeddedDir{}

	// register embeddedBox
	embedded.RegisterEmbeddedBox(`sql`, &embedded.EmbeddedBox{
		Name: `sql`,
		Time: time.Unix(1590313900, 0),
		Dirs: map[string]*embedded.EmbeddedDir{
			"": dir1,
		},
		Files: map[string]*embedded.EmbeddedFile{
			"settings.sql": file2,
			"tables.sql":   file3,
		},
	})
}
