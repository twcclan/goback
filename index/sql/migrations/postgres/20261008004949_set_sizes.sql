-- Drop index "files_versions" from table: "files"
DROP INDEX "files_versions";
-- Create index "files_versions" to table: "files"
CREATE INDEX "files_versions" ON "files" ("set_id", "ref", "size", "valid_from", "valid_until") WHERE (ref IS NOT NULL);
-- Modify "sets" table
ALTER TABLE "sets" ADD COLUMN "logical_size" bigint NULL, ADD COLUMN "kept_logical_size" bigint NULL, ADD COLUMN "unique_size" bigint NULL, ADD COLUMN "sizes_digest" bytea NULL;
