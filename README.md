## Run locally

Run `go run .` to use the local SQLite file `test.db`. Set `SQLLITE_FILE` to
choose another SQLite file. When `DATABASE_URL` is set, the app uses that
CockroachDB PostgreSQL URL instead. The Kubernetes deployment uses
`postgresql://root@cockroachdb-public.cockroach.svc.cluster.local:26257/defaultdb?sslmode=disable`.
GORM creates or updates the `events` and `rsvps` tables on startup.
The Fly configuration also requires a `DATABASE_URL` secret pointing to a
database reachable from Fly; the Kubernetes DNS name only works in the cluster.

## Build

`./build.sh <tag>` builds and pushes a tagged image; omitting the tag uses the
current commit. Both Kubernetes manifests currently point to
`paulgmiller/aggrovites:cockroach-20261004-1`.

## One-time data migration

The existing Kubernetes deployment reads from SQL Server. The one-time command
`/app migrate-data` copies all events and RSVPs from `MSSQL_DSN` into
`DATABASE_URL`. For a local SQLite source, set `SQLLITE_FILE` instead of
`MSSQL_DSN` and run `go run . migrate-data`.

1. Stop writes to the old app and back up the source database. Use an empty
   CockroachDB `defaultdb`; the command refuses to copy into populated tables.
2. Build and push the new image, replace the image tag in both manifests, then
   apply `migrate-data-job.yaml` before `deploy.yaml`.
3. Check the job logs for `Data migration complete` and verify the copied
   events and RSVPs. Then apply `deploy.yaml` to switch app traffic.

The command uses GORM to create the target schema and copies records in a
single destination transaction. It preserves IDs, timestamps, soft-deleted
rows, and RSVP links. It does not remove data from SQL Server. Keep the old app
stopped until the cutover completes so new writes cannot be missed.

## TODOs
* location/maps
* use cookies to remember invites? (ha passkeys?)
