CREATE TABLE IF NOT EXISTS `files`(
	`backup_set` TEXT NOT NULL,
	`path` TEXT NOT NULL,
	`mode` INTEGER NOT NULL,
	`timestamp` INTEGER NOT NULL,
	`size` INTEGER NOT NULL,
	`ref` BLOB NOT NULL CHECK(length(`ref`) = 32),
	PRIMARY KEY(`backup_set`, `path`, `timestamp`, `ref`)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS `commits`(
	`backup_set` TEXT NOT NULL,
	`ref` BLOB NOT NULL CHECK(length(`ref`) = 32),
	`timestamp` INTEGER NOT NULL,
	`tree` BLOB NOT NULL CHECK(length(`tree`) = 32),
	PRIMARY KEY(`backup_set`, `ref`)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS `commits_by_time` ON `commits`(`backup_set`, `timestamp`);
