## Run locally

Run `go run .` to use the local SQLite file `test.db`. Set `SQLLITE_FILE` to
choose another SQLite file. When `DATABASE_URL` is set, the app uses that
CockroachDB PostgreSQL URL instead. The Kubernetes deployment uses
`postgresql://root@cockroachdb-public.cockroach.svc.cluster.local:26257/vitesdb?sslmode=disable&default_query_exec_mode=simple_protocol`.
The connection uses pgx simple protocol to avoid a metadata-query failure
after the database rename.
GORM creates or updates the `events` and `rsvps` tables on startup.

## Build

`./build.sh <tag>` builds and pushes a tagged image; omitting the tag uses the
current commit. Update the image tag in `deploy.yaml` before applying the
Kubernetes deployment.

## TODOs
* location/maps
* use cookies to remember invites? (ha passkeys?)
* make things linkable.
