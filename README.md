# goback

Deduplicating, encrypted backups for one user and one store. An agent backs
directories up into a store: a local directory, a bucket on Google Cloud
Storage or anything S3-compatible, or a goback server that gathers several
agents into one store.

## How it stores data

- Objects are content-addressed by SHA-256, like git's: a commit points at a
  tree, a tree at files and subtrees, a file at the chunks it is made of.
- Files are cut into chunks with FastCDC, so an edit stores only the chunks it
  changed, and the same chunk is stored once across files, commits and sets.
- Objects are packed into archives, each with an index. A local SQLite or
  Postgres index answers queries, and can always be rebuilt from the archives.
- A backup only uploads what the store doesn't already hold, and a restore
  only downloads what the target directory doesn't already hold.

## Encryption

- **Store key (client side).** With `--store-key`, names and contents are
  encrypted before they leave the agent; a server never sees them. The key can
  be escrowed in the store under a passphrase (`goback key escrow`) and opened
  with `--passphrase`. Without the key or the passphrase, nothing can be
  restored.
- **At-rest key (server side).** With `--at-rest-key`, archives are sealed
  where they are stored, independently of the store key.

## Quick start

```sh
go build ./cmd/goback

goback key new my-store                 # writes store.key; keep a copy elsewhere

goback --storage /backups --store-key store.key --set home \
  commit new ~/

goback --storage /backups --store-key store.key --set home commit list

goback --storage /backups --store-key store.key --set home \
  commit restore ~/restored             # the latest commit; add an age like 24h for an older one
```

`--storage` also takes `gcs://bucket`, `s3://bucket?endpoint=…`, or
`goback://<secret>@host:port` for a goback server.

## Commands

| Command | Does |
| --- | --- |
| `commit` | back a directory up, list commits, restore, delete and undelete |
| `file` | list and restore single files and their versions |
| `set` | delete and undelete a set, show or change its retention |
| `pin` | keep a commit regardless of retention |
| `key` | make, derive, escrow and recover store keys; make at-rest keys |
| `gc` | mark what live commits and pins reach, and rewrite archives that are mostly dead |
| `maintain` | finalize idle archives, compact, retire expired commits, build presence filters |
| `scrub`, `repair` | rehash every stored object, and rewrite the archives holding corrupt ones |
| `fix` | rebuild the index from the store |
| `postgres` | back up a Postgres cluster with point-in-time recovery |
| `server` | serve one store to many agents over gRPC, with TLS and an operator API |

`goback <command> --help` describes each one.

## Retention

A set keeps its commits by brackets, newest first:

```sh
goback --storage /backups set retention home \
  --keep hourly=14d --keep daily=60d --keep weekly=12w --keep monthly
```

Commits the policy lets go are retired, then tombstoned after a hold window.
Once no live commit or pin reaches an object, garbage collection drops it.

## A server for several agents

```sh
goback --storage gcs://my-bucket server --secret-file secret \
  --tls-cert cert.pem --tls-key key.pem
```

Every agent presents the shared secret and its own `--agent-id`. The server
runs maintenance itself: it sweeps, compacts, retires and collects on its own
intervals.

## Postgres

`goback postgres` takes base backups and ships WAL into two sets, and restores
a cluster to any moment in between. [contrib/postgres](contrib/postgres/README.md)
runs it with Docker Compose.

## Development

```sh
go build ./...
go test ./...
```

Some tests start Postgres and MinIO in Docker through testcontainers.
