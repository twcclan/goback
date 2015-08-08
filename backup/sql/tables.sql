CREATE TABLE IF NOT EXISTS `chunks`(
	`sum`	BLOB NOT NULL,
	PRIMARY KEY(sum)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS `fileInfo`(
	`name` TEXT NOT NULL,
	`mode` INTEGER NOT NULL,
	--`user` TEXT NOT NULL,
	--`group` TEXT NOT NULL,
	`timestamp` INTEGER NOT NULL,
	`size` INTEGER NOT NULL,
	'chunk' BLOB NOT NULL,
	'data' BLOB NOT NULL,
	PRIMARY KEY(name, timestamp),
	FOREIGN KEY(chunk) REFERENCES chunks(sum),
	FOREIGN KEY(data) REFERENCES chunks(sum)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS `snapshotInfo`(
	`timestamp` INTEGER NOT NULL,
	'chunk' BLOB NOT NULL,
	'data' BLOB NOT NULL,
	PRIMARY KEY(timestamp),
	FOREIGN KEY(chunk) REFERENCES chunks(sum),
	FOREIGN KEY(data) REFERENCES chunks(sum)
) WITHOUT ROWID;
