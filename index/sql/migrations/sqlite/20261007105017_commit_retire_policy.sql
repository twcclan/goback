-- Add column "retire_policy" to table: "commits"
ALTER TABLE `commits` ADD COLUMN `retire_policy` text NULL;
