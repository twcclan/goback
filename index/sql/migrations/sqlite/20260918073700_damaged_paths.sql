-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_sets" table
CREATE TABLE `new_sets` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `agent_id` text NULL, `state` text NOT NULL DEFAULT ('active'), `retention_policy` text NULL, `retention_paused` bool NOT NULL DEFAULT (false), `erase` bool NOT NULL DEFAULT (false), `rescan` bool NOT NULL DEFAULT (false));
-- Copy rows from old table "sets" to new temporary table "new_sets"
INSERT INTO `new_sets` (`id`, `name`, `agent_id`, `state`, `retention_policy`, `retention_paused`, `erase`) SELECT `id`, `name`, `agent_id`, `state`, `retention_policy`, `retention_paused`, `erase` FROM `sets`;
-- Drop "sets" table after copying rows
DROP TABLE `sets`;
-- Rename temporary table "new_sets" to "sets"
ALTER TABLE `new_sets` RENAME TO `sets`;
-- Create index "set_name" to table: "sets"
CREATE UNIQUE INDEX `set_name` ON `sets` (`name`);
-- Create "damaged_paths" table
CREATE TABLE `damaged_paths` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `path` text NOT NULL, `found_at` datetime NOT NULL, `set_id` integer NOT NULL, CONSTRAINT `damaged_paths_sets_damaged` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION);
-- Create index "damagedpath_set_id_path" to table: "damaged_paths"
CREATE UNIQUE INDEX `damagedpath_set_id_path` ON `damaged_paths` (`set_id`, `path`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
