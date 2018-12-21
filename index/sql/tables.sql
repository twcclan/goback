CREATE TABLE IF NOT EXISTS `files`(
	`path` TEXT NOT NULL,
	`mode` INTEGER NOT NULL,
	--`user` TEXT NOT NULL,
	--`group` TEXT NOT NULL,
	`timestamp` INTEGER NOT NULL,
	`size` INTEGER NOT NULL,
	`ref` BLOB NOT NULL,
	`commit` BLOB NOT NULL,
	PRIMARY KEY(`path`, `commit`)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS `commits`(
  `ref` BLOB NOT NULL,
	`timestamp` INTEGER NOT NULL,
	`tree` BLOB NOT NULL,
	PRIMARY KEY(timestamp)
) WITHOUT ROWID;
