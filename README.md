## Run locally

Run `go run .` to use the local SQLite file `test.db`. Set `SQLLITE_FILE` to
choose another SQLite file. When `DATABASE_URL` is set, the app uses that
CockroachDB PostgreSQL URL instead. The Kubernetes deployment reads
`DATABASE_URL` from key `DATABASE_URL` in Secret `cockroachdb-connection` in
namespace `aggrovites`. The Secret contains:

```text
postgresql://root:PASSWORD@cockroachdb-secure-public.cockroach.svc.cluster.local:26257/aggrovites?sslmode=require
```

The Secret was provisioned during the secure database migration in `zebu`.
The generated root password is saved in `cockroach/cockroachdb-root` and reused
by subsequent database deployments. Keep passwords out of Git. The application
needs no certificate mounts; `sslmode=require` encrypts the connection but does
not verify the database server's identity.

GORM creates or updates the `events`, `rsvps`, and `invitation_drafts` tables on
startup. Existing events keep their original plain-text display.

## AI invitations

Set `OPENAI_API_KEY` in the server environment to enable invitation generation.
The optional model settings are:

| Variable | Default |
| --- | --- |
| `OPENAI_TEXT_MODEL` | `gpt-6-luna` |
| `OPENAI_IMAGE_MODEL` | `gpt-image-2.5-flare` |

The text model uses the Responses API with structured output containing Markdown,
an explicitly stated location, and an illustration prompt. The image model uses
the Images API (`1024x1024`, low quality); the app stores a 512-pixel JPEG.
Your OpenAI project must have access to the configured models. API keys stay on
the server and are never sent to the browser.

Hosts enter their description, date, and time, then review the formatted
invitation before publishing. To make changes, use the browser back button,
edit the details, and create a new preview. Aggrovites generates aggressive,
profane invitations; Nicevites generates extremely polite ones. The creation
site's voice is retained even if guests open the event on the other hostname.
For local Nicevites testing, open `http://localhost:9000/?host=nice`.

URLs in the invitation become clickable. A stated venue or address becomes a
Google Maps directions link; no Google API key or geocoding service is required.
Guests see the formatted invitation, illustration, and RSVP form. Markdown is
stored internally for rendering; no editor or download is exposed.

Drafts and artwork live in the database, so no writable filesystem or external
image hosting is needed. Draft URLs are private bearer links: anyone with the
link can review or publish the draft until it expires after 24 hours. Expired
drafts are removed when another preview is created. Each creation submission
generates a new preview; viewing and publishing do not make API calls or change
the reviewed content. Repeated publication returns the same event. Generation
failures preserve the source for publication, or hosts can go back and try again.
Artwork is optional when publishing.
Without `OPENAI_API_KEY`, hosts can still preview and publish their own Markdown.

`deploy.yaml` optionally reads `OPENAI_API_KEY` from the `openai-api` Kubernetes
Secret in namespace `aggrovites`. Provision that Secret with your normal secret
management workflow, then restart the deployment to enable AI. Without the Secret,
the app remains available in manual mode. Never put credentials in YAML or Git.
Override the model defaults with environment variables in the deployment if needed.

## Verify

Run `go test ./...` and `go vet ./...`. Tests use a temporary SQLite database and
mock OpenAI at the HTTP transport boundary; they make no paid API calls.

## Build

`./build.sh <tag>` builds and pushes a tagged image; omitting the tag uses the
current commit. Update the image tag in `deploy.yaml` before applying the
Kubernetes deployment.

## TODOs
* use cookies to remember invites? (ha passkeys?)
