-- Modify "settings" table
ALTER TABLE "settings" ADD COLUMN "policy_sequence" bigint NOT NULL DEFAULT 0;
