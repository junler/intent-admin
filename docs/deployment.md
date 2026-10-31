# Deployment

The production artifact is one Go executable with the Ant Design Pro frontend embedded. PostgreSQL and Redis are required. MinIO/S3 is optional when local file storage is selected.

## Compose

```sh
cp .env.example .env
docker compose up --build
```

The application listens on 8080. PostgreSQL, Redis and MinIO administration ports bind only to loopback. Compose builds MinIO from official source commit `07c3a429bfed433e49018cb0f78a52145d4bedeb` (release `RELEASE.2025-09-07T16-13-09Z`), avoiding dependence on a mutable third-party binary image. Its original console module is pinned to commit `2017f33b26e1cb632dd208ab7d91add1d06990fd` from the public Chainguard preservation mirror because the upstream console repository is private. The MinIO image carries its AGPL license. A small Go initializer creates the private `intent-admin` bucket. Application storage defaults to S3 with one-hour presigned URLs.

`STORAGE_PUBLIC_URL` must be the S3 URL browsers can reach, including the bucket path; the default is `http://localhost:9000/intent-admin`. The internal endpoint is `minio:9000`. The service signs the public endpoint correctly; changing a presigned URL's hostname afterward invalidates its signature. For a remote deployment, use the real HTTPS S3 hostname. For local-disk storage, set `STORAGE_DRIVER=local`, `STORAGE_URL_MODE=static` and `STORAGE_PUBLIC_URL=http://localhost:8080/api/public/files` before the first start. Bootstrap values fill an empty table once; later changes belong in the storage configuration page.

The included credentials and initial `admin` / `123456` account are for local evaluation. For production:

1. Set `APP_ENV=prod`; generate independent `JWT_SECRET` and `ENCRYPT_KEY` values with `openssl rand -base64 32`.
2. Choose unique PostgreSQL, Redis and MinIO credentials, and change the administrator password.
3. Put the application and object storage behind HTTPS. Set their public origins to the actual external URLs.
4. Set `mcp.oauth_issuer` to the canonical external HTTPS origin used by the application and MCP clients.
5. Back up PostgreSQL, uploaded objects, `app.encrypt_key` and `jwt.secret` together. Preserve Redis persistence if queued tasks, sessions and unexpired credential-compensation records must survive restarts.

Do not use `docker compose down -v` unless intentionally deleting this installation's databases, queue/session state and uploads. Normal `docker compose down` retains named volumes.

## Upgrade and release artifacts

Run `intent-admin migrate up` before starting the new binary. Migrations are embedded and versioned; configuration remains outside the binary. `intent-admin migrate seed` only fills missing initial records. Rollback migrations in a staging copy before using `migrate down`; schema rollbacks may remove tables. Business rollback uses changesets instead of migration rollback.

`make build` or the multi-stage Dockerfile builds the frontend and a static backend. Build metadata is available from `version`. The production container runs as UID/GID 65532 and contains no shell. `healthcheck` probes `/healthz` and validates the response code. The local upload volume must be writable by that UID.

When building behind a managed HTTPS proxy, supply its public CA without putting it in image layers:

```sh
docker build --secret id=proxy_ca,src=/path/to/proxy-ca.pem -t intent-admin:local .
docker build --secret id=proxy_ca,src=/path/to/proxy-ca.pem -f deploy/minio.Dockerfile -t intent-minio:2025-09-07 .
```

The normal Dockerfile works without this optional secret. Keep TLS verification enabled. Compose builds accept the same BuildKit secret through a local override file when needed.

## Workers and scheduler

`serve` embeds a worker by default. For a dedicated worker process:

```sh
INTENT_ADMIN_TASK_EMBEDDED=false intent-admin serve --config configs/config.yaml
intent-admin worker --config configs/config.yaml --concurrency 10
```

Set `task.scheduler=true` on exactly one serving process; workers consume queued work independently. Every replica must share PostgreSQL, Redis, the key prefix, signing/encryption keys and object storage. Queue submissions first enter the PostgreSQL task outbox in the business transaction; a dispatcher retries pending deliveries with stable task IDs. The current migrations define 29 application tables, excluding Goose migration metadata, including the outbox, cross-person approval records, OAuth state and private notifications. Task handlers come from the named registry; arbitrary commands and code are not accepted as job targets.

## MCP OAuth and Resources

Configure `mcp.oauth_issuer` in YAML or `INTENT_ADMIN_MCP_OAUTH_ISSUER` in the application environment. It is a canonical origin without a path, query or fragment. The sample and Compose default are `http://127.0.0.1:8080`; production uses HTTPS, for example `https://admin.example.com`. Use the same public origin when opening the consent page and configuring the MCP client. An empty setting disables OAuth endpoints; scoped PAT authentication remains available.

Clients use `/.well-known/oauth-protected-resource/mcp` and `/.well-known/oauth-authorization-server` for discovery. Keep these paths, `/oauth/*`, the consent frontend and `/mcp` reachable through the same canonical origin. Do not hide the discovery documents behind application-login middleware. Authorization uses public-client registration, PKCE S256, a one-use authorization code, explicit logged-in consent and resource-bound tokens. Access tokens default to 15 minutes; optional `offline_access` uses rotating refresh tokens within a grant lasting up to 30 days. Users can revoke their grants in the MCP authorizations page. Revocation, account status and permission changes are checked against live state. `mcp:read` does not permit writes, even if the account's roles include write permissions.

The Resources protocol exposes permission-filtered capabilities, dictionaries, entity knowledge, rules and the current principal's context. Every list/read request rechecks access, including requests over long-lived stdio sessions. Neither OAuth scopes nor Resources grant new business permissions. Critical operations and model/storage configuration writes retain their entry-point restrictions.

## Browser workflows and limits

The frontend is built from the official Ant Design Pro / Umi Max scaffold with shared layout, cards, forms and responsive tables. The header language picker persists Chinese or English locally and reloads the page using Umi's default locale behavior. A locale switch does not migrate user-entered business content.

XLSX imports are available for users, roles, parameters, dictionary data and demo items. Imports are create-only, require the entity's existing create permission, and reject existing unique keys. The limit is one file of at most 5 MB and 200 data rows, further reduced by `changeset.max_items_per_set` when applicable. A valid owner-bound preview expires after 10 minutes. Confirmation rechecks permissions, uniqueness, configuration and current data before committing the whole batch into one changeset. A validation or write failure leaves no partial batch. User passwords come from the server's initial-password setting; password/grant columns and credential-bearing parameters are rejected. Redis loss expires staged imports, so users upload again.

Cross-person plan approvals require a distinct assigned reviewer with `ai:plan:approve`, AI access and the original operations' permissions. Both participants' live permissions and risk ceilings are checked at request, approval and execution. The proposer executes the approved plan; approval cannot raise the available risk ceiling. Data or plan changes invalidate the reviewed snapshot and require a fresh approval. Pending requests and decisions have durable PostgreSQL records, with private recipient notifications. Approval expiry never exceeds the plan's expiry.

Changeset details support selected item IDs and an exact preview confirmation. Custom compensation groups remain atomic. Independent successful items are marked rolled back, conflicts retain their field names and reasons, and the parent shows partial, complete or failed rollback state. Each successful attempt creates a linked inverse changeset. Rolling it back restores both the business values and the corresponding original rollback states. Credential compensation depends on its unexpired encrypted Redis vault record; permanently purged export files cannot be restored.

## Reverse proxy

`deploy/nginx.conf` shows the relevant settings. Keep proxy buffering and caching disabled for AI SSE and MCP; allow long reads. Forward the host, client IP and scheme. Align the body-size limit with configured upload limits and allow multipart overhead for the 5 MB import limit. Terminate TLS at the proxy or ingress used by the deployment.

The build precompresses static assets to `.br` and `.gz`, and the binary serves whichever the client accepts, preferring brotli, with `Vary: Accept-Encoding`. Pass `Accept-Encoding` through unchanged; a proxy that strips it or re-compresses responses itself silently discards this and ships the uncompressed bundle.

## Observability and recovery

`/healthz` checks both PostgreSQL and Redis. Structured logs include request IDs and audit failures. The application UI provides online users, task logs, login/operation logs, server status, a bounded cache whitelist and an integrated queue console. Clearing sessions immediately revokes JWT access; personal tokens are separately revocable.

Queue management lists only the configured queues in the current Redis namespace. Grants such as `monitor:queue:list`, `monitor:queue:query`, `monitor:queue:retry`, `monitor:queue:archive`, `monitor:queue:cancel`, `monitor:queue:remove` and `monitor:queue:pause` control access. Mutations show a preview and are audited. Pause stops new task delivery; running work continues. Cancellation is best effort and cannot undo committed results. Internal export payloads and identities are redacted. Export retries use the original identity and recheck its current permissions; durable completion markers prevent completed business work from being replayed by queue controls.

Changesets record the actor, source, intent, plan, approval mode, approver and changed fields. Rollback compares current values, reports conflicts and continues with independent selected items. It produces another changeset, including an inverse vault reference when credentials are compensated. Retention also limits availability of those rollback credentials; expired credentials require manual reconfiguration. Rotating `jwt.secret` invalidates sessions and existing credential-vault records, so plan that rotation alongside credential recovery.
