-- Create "archives" table
CREATE TABLE `archives` (`id` text NOT NULL, `state` integer NOT NULL DEFAULT (0), `session_id` text NULL, PRIMARY KEY (`id`), CONSTRAINT `archives_sessions_archives` FOREIGN KEY (`session_id`) REFERENCES `sessions` (`id`) ON DELETE CASCADE);
-- Create index "archives_session" to table: "archives"
CREATE INDEX `archives_session` ON `archives` (`session_id`) WHERE session_id IS NOT NULL;
-- Create "commits" table
CREATE TABLE `commits` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `timestamp` datetime NOT NULL, `received_at` datetime NOT NULL, `tree` blob NOT NULL, `parent` blob NULL, `agent_id` text NOT NULL DEFAULT (''), `scan_start_ns` integer NOT NULL DEFAULT (0), `policy_version` integer NOT NULL DEFAULT (0), `consistent` bool NOT NULL DEFAULT (false), `presence` blob NULL, `partial` bool NOT NULL DEFAULT (false), `retained_by` text NOT NULL DEFAULT (''), `retire_at` datetime NULL, `deleted_at` datetime NULL, `expires_at` datetime NULL, `tombstoned_at` datetime NULL, `logical_size` integer NULL, `metadata` json NULL, `set_id` integer NOT NULL, CONSTRAINT `commits_sets_set` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `commits_ref_width` CHECK (length(ref) = 32), CONSTRAINT `commits_tree_width` CHECK (length(tree) = 32));
-- Create index "commitrow_ref" to table: "commits"
CREATE UNIQUE INDEX `commitrow_ref` ON `commits` (`ref`);
-- Create index "commitrow_set_id_received_at" to table: "commits"
CREATE INDEX `commitrow_set_id_received_at` ON `commits` (`set_id`, `received_at`);
-- Create index "commits_presence" to table: "commits"
CREATE INDEX `commits_presence` ON `commits` (`set_id`) WHERE presence IS NOT NULL;
-- Create index "commits_expiring" to table: "commits"
CREATE INDEX `commits_expiring` ON `commits` (`expires_at`) WHERE tombstoned_at IS NULL AND expires_at IS NOT NULL;
-- Create "deleted_refs" table
CREATE TABLE `deleted_refs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `tombstoned_at` datetime NOT NULL, CONSTRAINT `deleted_refs_ref_width` CHECK (length(ref) = 32));
-- Create index "deletedref_ref" to table: "deleted_refs"
CREATE UNIQUE INDEX `deletedref_ref` ON `deleted_refs` (`ref`);
-- Create "files" table
CREATE TABLE `files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `path` text NOT NULL, `dir` text NOT NULL, `valid_from` datetime NOT NULL, `valid_until` datetime NULL, `ref` blob NOT NULL, `mtime_ns` integer NOT NULL, `mode` integer NOT NULL, `user` text NOT NULL, `group` text NOT NULL, `size` integer NOT NULL, `set_id` integer NOT NULL, CONSTRAINT `files_sets_files` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `files_ref_width` CHECK (length(ref) = 32));
-- Create index "file_set_id_path_valid_from" to table: "files"
CREATE UNIQUE INDEX `file_set_id_path_valid_from` ON `files` (`set_id`, `path`, `valid_from`);
-- Create index "files_open" to table: "files"
CREATE INDEX `files_open` ON `files` (`set_id`, `dir`) WHERE valid_until IS NULL;
-- Create "objects" table
CREATE TABLE `objects` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `start` integer NOT NULL, `length` integer NOT NULL, `type` integer NOT NULL, `archive_id` text NOT NULL, CONSTRAINT `objects_archives_objects` FOREIGN KEY (`archive_id`) REFERENCES `archives` (`id`) ON DELETE CASCADE, CONSTRAINT `objects_ref_width` CHECK (length(ref) = 32));
-- Create index "object_ref_archive_id" to table: "objects"
CREATE UNIQUE INDEX `object_ref_archive_id` ON `objects` (`ref`, `archive_id`);
-- Create "pins" table
CREATE TABLE `pins` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `target` blob NOT NULL, `received_at` datetime NOT NULL, `deleted_at` datetime NULL, CONSTRAINT `pins_ref_width` CHECK (length(ref) = 32), CONSTRAINT `pins_target_width` CHECK (length(target) = 32));
-- Create index "pin_ref" to table: "pins"
CREATE UNIQUE INDEX `pin_ref` ON `pins` (`ref`);
-- Create index "pins_target" to table: "pins"
CREATE INDEX `pins_target` ON `pins` (`target`) WHERE deleted_at IS NULL;
-- Create "sessions" table
CREATE TABLE `sessions` (`id` text NOT NULL, `agent_id` text NOT NULL, `backup_set` text NOT NULL, `started_at` datetime NOT NULL, `last_seen` datetime NOT NULL, `restore_ref` blob NULL, PRIMARY KEY (`id`));
-- Create "sets" table
CREATE TABLE `sets` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `agent_id` text NULL, `state` text NOT NULL DEFAULT ('active'), `retention_policy` text NULL, `retention_paused` bool NOT NULL DEFAULT (false), `erase` bool NOT NULL DEFAULT (false));
-- Create index "set_name" to table: "sets"
CREATE UNIQUE INDEX `set_name` ON `sets` (`name`);
-- Create "set_refs" table
CREATE TABLE `set_refs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `set_id` integer NOT NULL, CONSTRAINT `set_refs_sets_refs` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `set_refs_ref_width` CHECK (length(ref) = 32));
-- Create index "setref_set_id_ref" to table: "set_refs"
CREATE UNIQUE INDEX `setref_set_id_ref` ON `set_refs` (`set_id`, `ref`);
-- Create index "setref_ref" to table: "set_refs"
CREATE INDEX `setref_ref` ON `set_refs` (`ref`);
-- Create "settings" table
CREATE TABLE `settings` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `policy` text NULL, `policy_version` integer NOT NULL DEFAULT (0), `key_acknowledged_at` datetime NULL, `retention_policy` text NULL, `hold_days` integer NOT NULL DEFAULT (14), `trash_days` integer NOT NULL DEFAULT (14));
-- Create "trees" table
CREATE TABLE `trees` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `path` text NOT NULL, `dir` text NOT NULL, `valid_from` datetime NOT NULL, `valid_until` datetime NULL, `ref` blob NOT NULL, `set_id` integer NOT NULL, CONSTRAINT `trees_sets_trees` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `trees_ref_width` CHECK (length(ref) = 32));
-- Create index "tree_set_id_path_valid_from" to table: "trees"
CREATE UNIQUE INDEX `tree_set_id_path_valid_from` ON `trees` (`set_id`, `path`, `valid_from`);
-- Create index "trees_open" to table: "trees"
CREATE INDEX `trees_open` ON `trees` (`set_id`, `dir`) WHERE valid_until IS NULL;
