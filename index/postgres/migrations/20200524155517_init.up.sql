BEGIN;

CREATE TABLE IF NOT EXISTS sets
(
    "id"   serial primary key,
    "name" text unique not null
);

CREATE TABLE IF NOT EXISTS commits
(
    "ref"       bytea primary key,
    "timestamp" timestamptz not null,
    "tree"      bytea       not null,
    "set_id"    integer     not null references sets (id)
);

CREATE TABLE IF NOT EXISTS trees
(
    "ref"    bytea   not null,
    "path"   text    not null,
    "set_id" integer not null references sets (id),
    primary key ("ref", "path", "set_id")
);

CREATE TABLE IF NOT EXISTS files
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

COMMIT;