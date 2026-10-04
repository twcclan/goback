-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_settings" table
CREATE TABLE `new_settings` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `policy` text NULL, `policy_version` integer NOT NULL DEFAULT (0), `key_acknowledged_at` datetime NULL, `retention_policy` text NULL, `trash_days` integer NOT NULL DEFAULT (14), `policy_sequence` integer NOT NULL DEFAULT (0));
-- Copy rows from old table "settings" to new temporary table "new_settings"
INSERT INTO `new_settings` (`id`, `policy`, `policy_version`, `key_acknowledged_at`, `retention_policy`, `trash_days`, `policy_sequence`) SELECT `id`, `policy`, `policy_version`, `key_acknowledged_at`, `retention_policy`, `trash_days`, `policy_sequence` FROM `settings`;
-- Drop "settings" table after copying rows
DROP TABLE `settings`;
-- Rename temporary table "new_settings" to "settings"
ALTER TABLE `new_settings` RENAME TO `settings`;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
-- Retired commits no longer wait out a hold window
UPDATE `commits` SET `expires_at` = `retire_at` WHERE `retire_at` IS NOT NULL AND `deleted_at` IS NULL AND `tombstoned_at` IS NULL;
