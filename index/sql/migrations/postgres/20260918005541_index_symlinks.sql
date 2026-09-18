-- Modify "files" table
ALTER TABLE "files" ALTER COLUMN "ref" DROP NOT NULL, ADD COLUMN "type" bigint NOT NULL DEFAULT 0, ADD COLUMN "link_target" bytea NULL;
