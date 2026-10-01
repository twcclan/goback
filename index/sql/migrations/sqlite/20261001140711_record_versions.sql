-- Add column "created_at" to table: "archives"
ALTER TABLE `archives` ADD COLUMN `created_at` datetime NULL;
-- Add column "carried_time" to table: "objects"
ALTER TABLE `objects` ADD COLUMN `carried_time` integer NULL;
-- Add column "carried_offset" to table: "objects"
ALTER TABLE `objects` ADD COLUMN `carried_offset` integer NULL;
