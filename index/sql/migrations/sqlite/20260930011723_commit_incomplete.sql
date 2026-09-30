-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_commits" table
CREATE TABLE `new_commits` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `ref` blob NOT NULL, `timestamp` datetime NOT NULL, `received_at` datetime NOT NULL, `tree` blob NOT NULL, `parent` blob NULL, `agent_id` text NOT NULL DEFAULT (''), `scan_start_ns` integer NOT NULL DEFAULT (0), `policy_version` integer NOT NULL DEFAULT (0), `consistent` bool NOT NULL DEFAULT (false), `presence` blob NULL, `partial` bool NOT NULL DEFAULT (false), `incomplete` bool NOT NULL DEFAULT (false), `retained_by` text NOT NULL DEFAULT (''), `retire_at` datetime NULL, `deleted_at` datetime NULL, `expires_at` datetime NULL, `tombstoned_at` datetime NULL, `logical_size` integer NULL, `metadata` json NULL, `set_id` integer NOT NULL, CONSTRAINT `commits_sets_set` FOREIGN KEY (`set_id`) REFERENCES `sets` (`id`) ON DELETE NO ACTION, CONSTRAINT `commits_ref_width` CHECK (length(ref) = 32), CONSTRAINT `commits_tree_width` CHECK (length(tree) = 32));
-- Copy rows from old table "commits" to new temporary table "new_commits"
INSERT INTO `new_commits` (`id`, `ref`, `timestamp`, `received_at`, `tree`, `parent`, `agent_id`, `scan_start_ns`, `policy_version`, `consistent`, `presence`, `partial`, `retained_by`, `retire_at`, `deleted_at`, `expires_at`, `tombstoned_at`, `logical_size`, `metadata`, `set_id`) SELECT `id`, `ref`, `timestamp`, `received_at`, `tree`, `parent`, `agent_id`, `scan_start_ns`, `policy_version`, `consistent`, `presence`, `partial`, `retained_by`, `retire_at`, `deleted_at`, `expires_at`, `tombstoned_at`, `logical_size`, `metadata`, `set_id` FROM `commits`;
-- Drop "commits" table after copying rows
DROP TABLE `commits`;
-- Rename temporary table "new_commits" to "commits"
ALTER TABLE `new_commits` RENAME TO `commits`;
-- Create index "commitrow_ref" to table: "commits"
CREATE UNIQUE INDEX `commitrow_ref` ON `commits` (`ref`);
-- Create index "commitrow_set_id_received_at" to table: "commits"
CREATE INDEX `commitrow_set_id_received_at` ON `commits` (`set_id`, `received_at`);
-- Create index "commits_presence" to table: "commits"
CREATE INDEX `commits_presence` ON `commits` (`set_id`) WHERE presence IS NOT NULL;
-- Create index "commits_expiring" to table: "commits"
CREATE INDEX `commits_expiring` ON `commits` (`expires_at`) WHERE tombstoned_at IS NULL AND expires_at IS NOT NULL;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
