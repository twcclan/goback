-- Create "reindexes" table
CREATE TABLE "reindexes" ("table_name" character varying NOT NULL, "churn" bigint NOT NULL, "stats_reset" timestamptz NULL, "reindexed_at" timestamptz NOT NULL, PRIMARY KEY ("table_name"));
