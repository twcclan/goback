-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_sets" table
CREATE TABLE `new_sets` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `state` text NOT NULL DEFAULT ('active'), `retention_policy` text NULL, `retention_paused` bool NOT NULL DEFAULT (false), `erase` bool NOT NULL DEFAULT (false), `rescan` bool NOT NULL DEFAULT (false), `physical_size` integer NULL);
-- Copy rows from old table "sets" to new temporary table "new_sets"
INSERT INTO `new_sets` (`id`, `name`, `state`, `retention_policy`, `retention_paused`, `erase`, `rescan`, `physical_size`) SELECT `id`, `name`, `state`, `retention_policy`, `retention_paused`, `erase`, `rescan`, `physical_size` FROM `sets`;
-- Drop "sets" table after copying rows
DROP TABLE `sets`;
-- Rename temporary table "new_sets" to "sets"
ALTER TABLE `new_sets` RENAME TO `sets`;
-- Create index "set_name" to table: "sets"
CREATE UNIQUE INDEX `set_name` ON `sets` (`name`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
