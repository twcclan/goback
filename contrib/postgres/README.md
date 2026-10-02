# Postgres backups with docker compose

`compose.yaml` backs up a Postgres container into a GCS bucket:

- Postgres archives each finished WAL file into a spool with `goback postgres archive`.
- [Ofelia](https://github.com/mcuadros/ofelia) runs inside the Postgres container:
  - `goback postgres wal` every minute, which commits the spool to the `db-wal` set.
  - `goback postgres base` daily, which streams `pg_basebackup` into the `db-base` set.
- goback reaches the Postgres container through the `goback-bin` volume. A one-shot service copies the static binary there on every `docker compose up`, so `docker compose build` followed by `docker compose up -d` upgrades it.

## Setup

1. Replace `BUCKET` in `compose.yaml` with your bucket.
2. Put a service account key with access to the bucket in `gcp-credentials.json`.
3. Create the store key with `goback key new <store name>` (it writes `store.key`) and move it to `goback.key`. Keep a copy elsewhere: without it, nothing can be restored.
4. Put the Postgres password in `postgres-password`.
5. Run `docker compose up -d`.

Details:

- **Point-in-time recovery window.** `postgres wal` keeps WAL back to the oldest base backup within `--window` (14 days by default). Base backups are kept by the `db-base` set's retention.
- **WAL retention during a base backup.** A base backup carries the WAL it needs (`-X fetch`). Postgres must keep that WAL until the backup ends, so `wal_keep_size` has to cover the WAL written while one runs.
- **Tablespaces.** Clusters with extra tablespaces aren't supported, because `pg_basebackup` writes only a single tablespace to a stream.
- **`init: true` is required.** The jobs run inside the Postgres container. Without an init process, an orphaned child is reparented to the postmaster, and when that child dies the postmaster restarts the cluster.
- **Ofelia's scope.** Ofelia schedules every labeled container on the Docker host. Give the jobs host-unique names when you back up several clusters.

## Restore

Restore into an empty directory, then start Postgres on it:

```sh
docker compose exec -u postgres postgres \
  /opt/goback/goback --storage 'gcs://BUCKET?index=/var/lib/goback/archives-restore' \
    --index /var/lib/goback/index --store-key /run/secrets/goback-key \
    postgres restore --base-set db-base --wal-set db-wal --at 2026-10-02T14:30:00Z /var/lib/goback/restored
```

- Leave out `--at` to recover to the end of the WAL set.
- On start, Postgres fetches each WAL file it needs with `goback postgres wal-get`, which `restore` writes into `postgresql.auto.conf` as `restore_command`. When recovery reaches the target, Postgres promotes.
- The command finds the sets through the index, or through the set heads in the bucket when the index has none.
