-- Create "reindexes" table
CREATE TABLE `reindexes` (`table_name` text NOT NULL, `churn` integer NOT NULL, `stats_reset` datetime NULL, `reindexed_at` datetime NOT NULL, PRIMARY KEY (`table_name`));
