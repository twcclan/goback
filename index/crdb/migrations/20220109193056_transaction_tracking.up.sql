BEGIN;

CREATE TYPE transaction_status AS ENUM ('invalid', 'open', 'committed', 'discarded');

CREATE TABLE transactions
(
    id     text primary key,
    status transaction_status not null
);

CREATE TABLE transaction_archives
(
    id             text primary key,
    transaction_id int8 references transactions (id) on delete cascade

);

COMMIT;