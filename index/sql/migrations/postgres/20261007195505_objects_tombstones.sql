-- Create index "objects_tombstones" to table: "objects"
CREATE INDEX CONCURRENTLY "objects_tombstones" ON "objects" ("ref") WHERE (type = 5);
