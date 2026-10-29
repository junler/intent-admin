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
4. Back up PostgreSQL, uploaded objects and the encryption key together. Preserve Redis persistence if queued tasks and live sessions must survive restarts.

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

Set `task.scheduler=true` on exactly one serving process; workers consume queued work independently. Every replica must share PostgreSQL, Redis, the key prefix, signing/encryption keys and object storage. Queue submissions first enter the PostgreSQL task outbox in the business transaction; a dispatcher retries pending deliveries with stable task IDs. The outbox is one infrastructure table in addition to the 21 planned system tables and the demonstration table. Task handlers come from the named registry; arbitrary commands and code are not accepted as job targets.

## Reverse proxy

`deploy/nginx.conf` shows the relevant settings. Keep proxy buffering and caching disabled for AI SSE and MCP; allow long reads. Forward the host, client IP and scheme. Align the body-size limit with configured upload limits. Terminate TLS at the proxy or ingress used by the deployment.

## Observability and recovery

`/healthz` checks both PostgreSQL and Redis. Structured logs include request IDs and audit failures. The application UI provides online users, task logs, login/operation logs, server status and a bounded cache whitelist. Clearing sessions immediately revokes JWT access; personal tokens are separately revocable.

Changesets record the actor, source, intent, plan and changed fields. Rollback compares current values, reports conflicts and continues with independent items. It produces another changeset, including an inverse vault reference when credentials are compensated. Retention also limits availability of those rollback credentials; expired credentials require manual reconfiguration.
