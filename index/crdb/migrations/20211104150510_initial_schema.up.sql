BEGIN;

CREATE TABLE archives
(
    name   text primary key not null
);

CREATE TABLE objects
(
    ref        bytea   not null,
    start      integer not null,
    length     integer not null,
    type       integer not null,
    archive_id text     not null references archives (name) on delete cascade,

    primary key (ref, archive_id)
);

COMMIT;