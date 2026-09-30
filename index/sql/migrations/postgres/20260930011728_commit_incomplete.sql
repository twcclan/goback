-- Modify "commits" table
ALTER TABLE "commits" ADD COLUMN "incomplete" boolean NOT NULL DEFAULT false;
