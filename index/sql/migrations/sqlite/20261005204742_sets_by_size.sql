-- Create index "sets_by_size" to table: "sets"
CREATE INDEX `sets_by_size` ON `sets` (`physical_size` DESC, `name`);
