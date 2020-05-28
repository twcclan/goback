BEGIN;

CREATE TABLE IF NOT EXISTS archives
(
    "id"   serial primary key,
    "name" text not null
);

CREATE TABLE IF NOT EXISTS index_records
(
    "ref"        bytea   not null,
    "offset"     integer not null,
    "length"     integer not null,
    "type"       integer not null,
    "archive_id" integer not null references archives (id),

    primary key ("ref", "archive_id")
);

COMMIT;