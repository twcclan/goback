-- Drop index "files_versions" from table: "files"
DROP INDEX `files_versions`;
-- Create index "files_versions" to table: "files"
CREATE INDEX `files_versions` ON `files` (`set_id`, `ref`, `size`, `valid_from`, `valid_until`) WHERE ref IS NOT NULL;
-- Add column "logical_size" to table: "sets"
ALTER TABLE `sets` ADD COLUMN `logical_size` integer NULL;
-- Add column "kept_logical_size" to table: "sets"
ALTER TABLE `sets` ADD COLUMN `kept_logical_size` integer NULL;
-- Add column "unique_size" to table: "sets"
ALTER TABLE `sets` ADD COLUMN `unique_size` integer NULL;
-- Add column "sizes_digest" to table: "sets"
ALTER TABLE `sets` ADD COLUMN `sizes_digest` blob NULL;
