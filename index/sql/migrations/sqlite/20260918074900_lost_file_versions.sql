-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_files" table
CREATE TABLE `new_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `path` text NOT NULL, `dir` text NOT NULL, `valid_from` datetime NOT NULL, `valid_until` datetime NULL, `ref` blob NULL, `mtime_ns` integer NOT NULL, `mode` integer NOT NULL, `user` text NOT NULL, `group` text NOT NULL, `size` integer NOT NULL, `type` integer NOT NULL DEFAULT (0), `link_target` blob NULL, `lost` bool NOT NULL DEFAULT (false), `set_id` integer NOT NULL, CONSTRAINT `files_sets_files` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `files_ref_width` CHECK (length(ref) = 32));
-- Copy rows from old table "files" to new temporary table "new_files"
INSERT INTO `new_files` (`id`, `path`, `dir`, `valid_from`, `valid_until`, `ref`, `mtime_ns`, `mode`, `user`, `group`, `size`, `type`, `link_target`, `set_id`) SELECT `id`, `path`, `dir`, `valid_from`, `valid_until`, `ref`, `mtime_ns`, `mode`, `user`, `group`, `size`, `type`, `link_target`, `set_id` FROM `files`;
-- Drop "files" table after copying rows
DROP TABLE `files`;
-- Rename temporary table "new_files" to "files"
ALTER TABLE `new_files` RENAME TO `files`;
-- Create index "file_set_id_path_valid_from" to table: "files"
CREATE UNIQUE INDEX `file_set_id_path_valid_from` ON `files` (`set_id`, `path`, `valid_from`);
-- Create index "files_open" to table: "files"
CREATE INDEX `files_open` ON `files` (`set_id`, `dir`) WHERE valid_until IS NULL;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
