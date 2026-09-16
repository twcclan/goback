BEGIN;

ALTER TABLE commits DROP CONSTRAINT commits_ref_width;
ALTER TABLE commits DROP CONSTRAINT commits_tree_width;
ALTER TABLE trees DROP CONSTRAINT trees_ref_width;
ALTER TABLE files DROP CONSTRAINT files_ref_width;
ALTER TABLE objects DROP CONSTRAINT objects_ref_width;

COMMIT;
