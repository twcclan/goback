-- Create index "files_versions" to table: "files"
CREATE INDEX "files_versions" ON "files" ("set_id", "ref", "size") WHERE (ref IS NOT NULL);
