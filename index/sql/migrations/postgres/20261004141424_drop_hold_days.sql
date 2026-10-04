-- Modify "settings" table
ALTER TABLE "settings" DROP COLUMN "hold_days";
-- Retired commits no longer wait out a hold window
UPDATE "commits" SET "expires_at" = "retire_at" WHERE "retire_at" IS NOT NULL AND "deleted_at" IS NULL AND "tombstoned_at" IS NULL;
