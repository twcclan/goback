-- Modify "archives" table
ALTER TABLE "archives" ADD COLUMN "created_at" timestamptz NULL;
-- Modify "objects" table
ALTER TABLE "objects" ADD COLUMN "carried_time" bigint NULL, ADD COLUMN "carried_offset" bigint NULL;
