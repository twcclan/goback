-- Create index "objects_tombstones" to table: "objects"
CREATE INDEX `objects_tombstones` ON `objects` (`ref`) WHERE type = 5;
