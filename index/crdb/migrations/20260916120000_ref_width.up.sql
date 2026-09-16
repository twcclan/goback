BEGIN;

ALTER TABLE commits ADD CONSTRAINT commits_ref_width CHECK (length(ref) = 32);
ALTER TABLE commits ADD CONSTRAINT commits_tree_width CHECK (length(tree) = 32);
ALTER TABLE trees ADD CONSTRAINT trees_ref_width CHECK (length(ref) = 32);
ALTER TABLE files ADD CONSTRAINT files_ref_width CHECK (length(ref) = 32);
ALTER TABLE objects ADD CONSTRAINT objects_ref_width CHECK (length(ref) = 32);

COMMIT;
