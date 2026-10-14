## Run locally

Run `go run .` to use the local SQLite file `test.db`. Set `SQLLITE_FILE` to
choose another SQLite file. When `DATABASE_URL` is set, the app uses that
CockroachDB PostgreSQL URL instead. The Kubernetes deployment reads
`DATABASE_URL` from key `DATABASE_URL` in Secret `cockroachdb-connection` in
namespace `aggrovites`. The Secret contains:

```text
postgresql://root:PASSWORD@cockroachdb-secure-public.cockroach.svc.cluster.local:26257/vitesdb?sslmode=require
```

The Secret was provisioned during the secure database migration in `zebu`.
The generated root password is saved in `cockroach/cockroachdb-root` and reused
by subsequent database deployments. Keep passwords out of Git. The application
needs no certificate mounts; `sslmode=require` encrypts the connection but does
not verify the database server's identity.

GORM creates or updates the `events` and `rsvps` tables on startup.

## Build

`./build.sh <tag>` builds and pushes a tagged image; omitting the tag uses the
current commit. Update the image tag in `deploy.yaml` before applying the
Kubernetes deployment.

## TODOs
* location/maps
* use cookies to remember invites? (ha passkeys?)
* make things linkable.
