BEGIN;

CREATE TABLE sets
(
    "id"   serial primary key,
    "name" text unique not null
);

CREATE TABLE commits
(
    "ref"       bytea primary key,
    "timestamp" timestamptz not null,
    "tree"      bytea       not null,
    "set_id"    integer     not null references sets (id)
);

CREATE TABLE trees
(
    "ref"    bytea   not null,
    "path"   text    not null,
    "set_id" integer not null references sets (id),
    primary key ("ref", "path", "set_id")
);

CREATE TABLE files
(
    "path"      text        not null,
    "timestamp" timestamptz not null,
    "set_id"    integer     not null references sets (id),
    "ref"       bytea       not null,
    "mode"      integer     not null,
    "user"      text        not null,
    "group"     text        not null,
    "size"      bigint      not null,

    primary key ("path", "timestamp", "set_id")
);

CREATE TABLE archives
(
    "name" text primary key not null
);

CREATE TABLE objects
(
    "ref"        bytea   not null,
    "archive_id" text    not null references archives (name) on delete cascade,
    "start"      integer not null,
    "length"     integer not null,
    "type"       integer not null,
    "rank"       integer not null,

    primary key ("ref", "archive_id")
);

COMMENT ON TABLE objects IS 'All available objects';
COMMENT ON COLUMN objects.start IS 'The offset within the archive';
COMMENT ON COLUMN objects.length IS 'The length of the object in bytes within the archive';
COMMENT ON COLUMN objects.type IS 'The object type';
COMMENT ON COLUMN objects.rank IS 'The rank of the object within the archive (e.g. 1st, 2nd, .... 586th)';

COMMIT;