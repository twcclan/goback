-- Create index "objects_commits" to table: "objects"
CREATE INDEX CONCURRENTLY "objects_commits" ON "objects" ("ref") WHERE (type = 1);
