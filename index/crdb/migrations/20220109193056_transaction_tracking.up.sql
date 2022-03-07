BEGIN;

CREATE TYPE transaction_status AS ENUM ('invalid', 'open', 'committed', 'prepared', 'aborted');

CREATE TABLE transactions
(
    id     text primary key,
    status transaction_status not null,
    created_at timestamptz not null,
    updated_at timestamptz
);

COMMIT;