package postgres

import (
	"time"

	"github.com/GeertJohan/go.rice/embedded"
)

func init() {

	// define files
	file2 := &embedded.EmbeddedFile{
		Filename:    "20200524155517_init.down.sql",
		FileModTime: time.Unix(1590689864, 0),

		Content: string("BEGIN;\n\nDROP TABLE IF EXISTS commits, files, sets, trees;\n\nCOMMIT;"),
	}
	file3 := &embedded.EmbeddedFile{
		Filename:    "20200524155517_init.up.sql",
		FileModTime: time.Unix(1590689864, 0),

		Content: string("BEGIN;\n\nCREATE TABLE IF NOT EXISTS sets\n(\n    \"id\"   serial primary key,\n    \"name\" text unique not null\n);\n\nCREATE TABLE IF NOT EXISTS commits\n(\n    \"ref\"       bytea primary key,\n    \"timestamp\" timestamptz not null,\n    \"tree\"      bytea       not null,\n    \"set_id\"    integer     not null references sets (id)\n);\n\nCREATE TABLE IF NOT EXISTS trees\n(\n    \"ref\"    bytea   not null,\n    \"path\"   text    not null,\n    \"set_id\" integer not null references sets (id),\n    primary key (\"ref\", \"path\", \"set_id\")\n);\n\nCREATE TABLE IF NOT EXISTS files\n(\n    \"path\"      text        not null,\n    \"timestamp\" timestamptz not null,\n    \"set_id\"    integer     not null references sets (id),\n    \"ref\"       bytea       not null,\n    \"mode\"      integer     not null,\n    \"user\"      text        not null,\n    \"group\"     text        not null,\n    \"size\"      bigint      not null,\n\n    primary key (\"path\", \"timestamp\", \"set_id\")\n);\n\nCOMMIT;"),
	}
	file4 := &embedded.EmbeddedFile{
		Filename:    "20200528123333_pack index.down.sql",
		FileModTime: time.Unix(1590689773, 0),

		Content: string("BEGIN;\n\nDROP TABLE IF EXISTS archives, index_records;\n\nCOMMIT;"),
	}
	file5 := &embedded.EmbeddedFile{
		Filename:    "20200528123333_pack index.up.sql",
		FileModTime: time.Unix(1590689773, 0),

		Content: string("BEGIN;\n\nCREATE TABLE IF NOT EXISTS archives\n(\n    \"id\"   serial primary key,\n    \"name\" text not null\n);\n\nCREATE TABLE IF NOT EXISTS index_records\n(\n    \"ref\"        bytea   not null,\n    \"offset\"     integer not null,\n    \"length\"     integer not null,\n    \"type\"       integer not null,\n    \"archive_id\" integer not null references archives (id),\n\n    primary key (\"ref\", \"archive_id\")\n);\n\nCOMMIT;"),
	}

	// define dirs
	dir1 := &embedded.EmbeddedDir{
		Filename:   "",
		DirModTime: time.Unix(1590689864, 0),
		ChildFiles: []*embedded.EmbeddedFile{
			file2, // "20200524155517_init.down.sql"
			file3, // "20200524155517_init.up.sql"
			file4, // "20200528123333_pack index.down.sql"
			file5, // "20200528123333_pack index.up.sql"

		},
	}

	// link ChildDirs
	dir1.ChildDirs = []*embedded.EmbeddedDir{}

	// register embeddedBox
	embedded.RegisterEmbeddedBox(`migrations`, &embedded.EmbeddedBox{
		Name: `migrations`,
		Time: time.Unix(1590689864, 0),
		Dirs: map[string]*embedded.EmbeddedDir{
			"": dir1,
		},
		Files: map[string]*embedded.EmbeddedFile{
			"20200524155517_init.down.sql":       file2,
			"20200524155517_init.up.sql":         file3,
			"20200528123333_pack index.down.sql": file4,
			"20200528123333_pack index.up.sql":   file5,
		},
	})
}
