CREATE TABLE IF NOT EXISTS `objects`(
	`ref`	BLOB NOT NULL,
	`type` INTEGER NOT NULL,
	`indexed` INTEGER NOT NULL,
	PRIMARY KEY(ref)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS `fileInfo`(
	`path` TEXT NOT NULL,
	`mode` INTEGER NOT NULL,
	--`user` TEXT NOT NULL,
	--`group` TEXT NOT NULL,
	`timestamp` INTEGER NOT NULL,
	`size` INTEGER NOT NULL,
	'ref' BLOB NOT NULL,
	PRIMARY KEY(`path`, `timestamp`),
	FOREIGN KEY(ref) REFERENCES objects(ref)
	--FOREIGN KEY(data) REFERENCES chunks(sum)
) WITHOUT ROWID;


 --CREATE TABLE IF NOT EXISTS `snapshotInfo`(
	--`timestamp` INTEGER NOT NULL,
	--'chunk' BLOB NOT NULL,
	--'data' BLOB NOT NULL,
	--PRIMARY KEY(timestamp),
	--FOREIGN KEY(chunk) REFERENCES chunks(sum),
	--FOREIGN KEY(data) REFERENCES chunks(sum)
--) WITHOUT ROWID;
