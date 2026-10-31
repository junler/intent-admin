# Contributing to Intent

Intent is an MIT-licensed, capability-driven administration system. Keep domain-specific applications outside the core repository. The implementation plan describes the product and architectural decisions; the current code and tests define the shipped API.

## Development environment

Install Go 1.26+, Node.js 22+, npm, PostgreSQL 17 and Redis 7. The frontend uses the official Ant Design Pro v6 scaffold, Umi Max, ProLayout, ProTable and ProForm. Its npm lockfile is authoritative. Use the shared layout, cards and form controls so new pages follow the same spacing, responsive behavior and theme.

```sh
cp configs/config.example.yaml configs/config.yaml
# Set database credentials and generate the signing/encryption keys.
make install
make migrate-up
make dev
```

The API listens on port 8080 and Umi Max on 5173. `API_PROXY` changes the development API target. `make build` builds the frontend, embeds it in the Go binary and writes `bin/intent-admin`. `docker compose up --build` starts the full local stack, including a private MinIO bucket. See [deployment](docs/deployment.md) for key management and separate workers.

## Architecture and boundaries

- Register business operations in `internal/capability`; HTTP, AI and MCP all use that executor.
- Keep database policies in services. Feature packages bind typed inputs, previews, recording and compensation to capabilities.
- Use `dbx.DBFrom(ctx, db)` in queries and writes so operations join the ambient transaction. Generic dry-run and undo helpers do this too.
- Record every changed row explicitly. The write and its changeset must commit together.
- Use `dbx.AfterCommit` for cache invalidation. A failed nested transaction must not invalidate or refill authorization caches.
- Preview handlers are read-only. Their context rejects GORM writes.
- Configuration credentials use AES-256-GCM. Never put plaintext, ciphertext, passwords or token hashes into previews or changeset snapshots. Opaque, expiring vault references support credential compensation.
- AI/MCP-visible writes require a preview. Model/storage configuration writes, personal password changes and token creation remain closed to these entry points.

## Add a business module

Use this when starting a whole module. "Add a Capability" below covers adding
one operation to a module that already exists.

Name capabilities after the table — `shop.order.list` for `shop_order` — and
the shared infrastructure resolves the module on its own. Route evidence, MCP
entity filtering and the startup knowledge check all derive from that name, so
no central list needs editing.

1. **Migration.** Add a versioned SQL file in `migrations/`. Never edit a
   released version.
2. **Model and service.** `internal/model` for the row, `internal/service` for
   the policy. Query through `dbx.DBFrom(ctx, db)`.
3. **Feature package.** `internal/feature/<module>` registering the
   capabilities, each with `DryRun` and, for writes, `Undo`. See the worked
   example below; `internal/feature/demo` is the same thing end to end.
4. **Bootstrap.** One `<module>.Register(a.DB)` call in `internal/bootstrap`.
   Pass `a.Tasks` too if the module ships scheduler handlers.
5. **Knowledge.** Register an `EntityDef` in `internal/knowledge` describing
   the table, its key and label columns and any invariants. Startup logs a
   warning naming the capability if this is missed.
6. **Menu.** Seed the menu row and permission strings in a migration. The
   `component` value maps to `web/src/pages/<path>/index.tsx`.
7. **Page.** Add the page under `web/src/pages`; `npm run dev` regenerates the
   component registry. `CrudPage` takes `export` and `import` flags — set
   `import` only after adding the entity's column specification in
   `internal/feature/importer`.
8. **Coded columns.** If a column stores dictionary codes, add an
   `entity.column` entry to `web/src/components/diff/fieldMetadata.ts` so
   changeset and rollback views show labels instead of raw values.

Run `go test ./... && npm --prefix web run lint && npm --prefix web test`.

## Remove the example module

`demo` exists so the Capability layer can be read end to end. To drop it:
run `intent-admin demo clear --yes`, then delete `internal/feature/demo`,
`internal/service/demo.go`, `internal/model/demo_item.go`,
`web/src/pages/demo`, `migrations/00001_demo_item.sql` and its menu rows, the
`demo.Register` call in `internal/bootstrap`, the `demo_item` entries in
`internal/knowledge` and `web/src/components/diff/fieldMetadata.ts`, and the
`/demo/item` specification in `internal/feature/importer`. Nothing else in the
framework refers to it.

## Add a Capability

The existing demo service is a compact worked example. This feature adds another typed operation to change its remark, reusing the service's transaction and Recorder behavior:

```go
package notes

import (
    "context"
    "github.com/junler/intent-admin/internal/capability"
    "github.com/junler/intent-admin/internal/capability/dryrun"
    "github.com/junler/intent-admin/internal/capability/undo"
    "github.com/junler/intent-admin/internal/dbx"
    "github.com/junler/intent-admin/internal/model"
    "github.com/junler/intent-admin/internal/service"
    "gorm.io/gorm"
)

type Input struct {
    ItemID int64 `json:"itemId" uri:"itemId" validate:"required,gt=0"`
    Remark string `json:"remark" validate:"max=500"`
}

func Register(db *gorm.DB) {
    svc := service.NewDemoService(db)
    capability.Register(capability.Capability[Input, capability.Empty]{
        Meta: capability.Meta{
            Name: "demo.item.note", Summary: "修改示例备注",
            Perm: "demo:item:edit", Risk: capability.RiskMedium,
            Mutating: true, Reversible: true, Idempotent: true,
            HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/demo/item/:itemId/note"},
            AIEnabled: true, MCPEnabled: true,
            LogTitle: "示例备注", BizType: capability.BizUpdate,
        },
        Fn: func(ctx context.Context, in Input) (capability.Empty, error) {
            return svc.Update(ctx, service.DemoUpdateIn{
                ItemID: in.ItemID, Remark: &in.Remark,
            })
        },
        DryRun: func(ctx context.Context, in Input) (*capability.Preview, error) {
            return dryrun.ForUpdate(ctx, dbx.DBFrom(ctx, db), &model.DemoItem{},
                []any{in.ItemID}, map[string]any{"remark": in.Remark},
                dryrun.LabelOpt{Entity: "demo_item", IDField: "item_id", LabelField: "name", Route: "/demo/item"})
        },
        Undo: undo.RestoreFieldsFn(db, undo.Table{Name: "demo_item", IDField: "item_id"}),
    })
}
```

Call `notes.Register(db)` from bootstrap. The shared adapter generates the HTTP operation, a POST preview route, OpenAPI schemas and AI/MCP descriptions. Add a menu permission for `demo:item:edit` when the operation has a page action.

A new service performs its write and reports only changed fields:

```go
before := map[string]any{"status": current.Status}
after := map[string]any{"status": requestedStatus}
if err := dbx.DBFrom(ctx, db).Model(&current).Updates(after).Error; err != nil {
    return err
}
capability.RecorderFrom(ctx).Update("demo_item", capability.ID(current.ItemID), current.Name, before, after)
```

Feature packages may perform this recording around a service result to keep the service independent of capability types. Use `Create`, `Delete` or `Skip` for their corresponding operations. Supply `SetUndo` once for an atomic custom group; its Undo implementation must return inverse compensation through its fresh Recorder to support rollback of rollback. Set `Reversible: false` for operations that cannot be compensated, and explain that fact in the preview.

Use explicit risks: low for reads, medium for a single change, high for deletion/bulk authorization, critical for irreversible administration. Missing risk fails closed. Add meaningful Chinese examples, field descriptions and business invariants to the knowledge registry.

## Extension contracts

XLSX imports in `internal/feature/importer` compose the ordinary create capabilities for users, roles, parameters, dictionary data and demo items. Keep them create-only and permission-equivalent to the original capability. A file accepts at most 200 data rows and 5 MB, subject to a lower configured changeset limit. Preview is read-only, owner-bound and valid for 10 minutes. Confirmation rechecks the fingerprint and all rows inside one transaction; any failure rolls back the batch. Do not accept password/grant columns, credential parameters, formulas or arbitrary nested input. Add a supported entity through an explicit column specification and its reversible create operation.

Cross-person approvals in `internal/plan` bind a different assigned reviewer to a saved plan fingerprint. Keep request, decision and execution limited to the relevant logged-in people. Both participants need the original permissions and current AI risk allowance; the reviewer also needs `ai:plan:approve`. Approval is not a grant. Changes to arguments, resolved references, data, permissions or risk require fresh review. Preserve `Actor.ApprovedBy` through changesets and audits. Private workflow notifications join the transaction through `internal/notification`; never replace them with public notices.

Selective compensation in `internal/changeset` accepts `{id, itemIds, previewHash}`. Omitting `itemIds` keeps the full remaining-items operation; an explicit empty array is invalid. The preview supplies `confirmationHash`; pass it back as `previewHash` for a selection. Validate that every item belongs to that changeset, reject duplicates, keep each `undoGroup` atomic and retain conflict fields for retries. Completed selections are idempotent. Update both item and parent statuses, persist linked inverse changesets and keep source permissions/risk checks active through rollback of rollback. Custom Undo implementations must produce fresh inverse payloads without exposing credentials.

The integrated queue console uses `internal/task` and `internal/feature/monitor`. Enforce the configured queue whitelist and Redis namespace on every read and mutation. Preview and audit controls without exposing internal export payloads or changing their original identity. Keep durable completion checks when retrying or deleting queue records. Cancellation is best effort; queue controls cannot roll back committed business work.

MCP OAuth in `internal/oauth` uses an explicitly configured canonical `mcp.oauth_issuer`, PKCE S256, public-client registration and logged-in consent. An empty issuer disables OAuth while PATs remain supported. Keep redirect/resource validation, refresh rotation, grant revocation and live permission resolution intact. A read-only grant must remain read-only through generic tool wrappers and confirmations. Resources in `internal/adapter/mcpx/resources.go` must authenticate and filter every listing/read, including on a long-lived session; a previously visible URI is not proof of current access.

For frontend copy, use `t` from `@/i18n` and add English values to the relevant catalogue under `web/src/i18n/messages`. Keep protocol strings, permission checks, stored values and user-entered content out of translation. Umi supplies the Chinese/English locale picker and reloads the document on a switch; do not add a second locale store. Use templates for interpolated UI messages and `localizeMessage` only for known server/product messages. Run `npm --prefix web run i18n:check` alongside tests and type checking.

UI form assistance uses `list_forms`, `describe_form` and `fill_form` to prepare browser-only drafts. Shared CRUD editors support creation, record-bound updates and row-action forms automatically. Register independent forms with `useAiForm`, declaring only editable field names and a field-setting callback; never pass a submit handler or current field values. Drafts are account-bound, expire after two minutes and are consumed once. Final writes continue through the ordinary human HTTP capability executor, including previews, permissions and audit. Credentials must not be placed in URLs, browser storage, plans or retained model tool-call history.

## Validation

```sh
make test
make test-web
make lint
npm --prefix web run i18n:check
make build
```

Integration tests require disposable databases; some tests truncate tables. Never point them at development data that matters or production. CI creates isolated databases and runs all suites. To run plans, cross-person approvals and selective rollback locally, create a disposable database and provide PostgreSQL plus Redis:

```sh
export INTENT_ADMIN_TEST_DSN='host=127.0.0.1 port=5432 user=postgres password=test-only dbname=intent_plan_integration sslmode=disable'
export INTENT_ADMIN_REDIS_ADDR='127.0.0.1:6379'
go test ./internal/plan ./internal/changeset -v -count=1

INTENT_ADMIN_OAUTH_TEST_DSN="$INTENT_ADMIN_TEST_DSN" \
  go test ./internal/oauth -v -count=1
INTENT_ADMIN_NOTIFICATION_DSN="$INTENT_ADMIN_TEST_DSN" \
  go test ./internal/notification -v -count=1
```

The plan, changeset and OAuth suites create isolated schemas. The import suite creates and drops its own database and requires database-creation rights; its configured database name must end in `_integration`. The notification suite requires an `_integration` database and removes its own fixture rows. Additional opt-in suites use the following flags with the disposable database and Redis configuration from `configs/config.example.yaml` or environment variables:

| Suite | Command and required flag |
| --- | --- |
| Authentication and HTTP | `INTENT_ADMIN_INTEGRATION=1 go test ./internal/bootstrap -v -count=1` |
| System permissions | `INTENT_ADMIN_SYSTEM_INTEGRATION=1 go test ./internal/feature/system -v -count=1` |
| Model/storage configuration and files | `INTENT_ADMIN_RESOURCE_INTEGRATION=1 go test ./internal/feature/resources -v -count=1` |
| Create-only XLSX import | `INTENT_ADMIN_IMPORT_INTEGRATION=1 go test ./internal/feature/importer -v -count=1` |
| Queue controls, scheduler and async exports | `INTENT_ADMIN_TASK_INTEGRATION=1 go test ./internal/task -v -count=1` |

Set `INTENT_ADMIN_DB_NAME`, `INTENT_ADMIN_DB_PASSWORD` and, when needed, `INTENT_ADMIN_DB_PORT` for those database-backed suites. The full commands and isolated database names are in [the CI workflow](.github/workflows/ci.yml). Real S3 tests additionally accept `INTENT_ADMIN_MINIO_TEST_ENDPOINT`, `INTENT_ADMIN_MINIO_TEST_PUBLIC_ENDPOINT`, `INTENT_ADMIN_MINIO_TEST_ACCESS_KEY` and `INTENT_ADMIN_MINIO_TEST_SECRET_KEY`; use a disposable private MinIO bucket environment.

Test permission failures, risk limits, before/after conflicts, transaction rollback and resource ownership when changing those behaviors. Extension tests also cover partial rollback and inverse credential compensation, stale approval/import confirmations, OAuth scope and refresh-token boundaries, resource visibility after revocation, and queue namespace/completion protections. New low-impact presentation changes do not need tests that merely repeat the implementation.

## Pull requests

Use a feature branch and a Conventional Commit title such as `feat: add notice expiry` or `fix: reject stale rollback`. Include the problem, resulting behavior, validation and migration instructions. Add backward-compatible migrations; do not rewrite an already released migration. The current versioned migrations define 29 application tables, excluding Goose metadata, including approvals, OAuth state, private notifications and the durable task outbox. Keep secrets and generated build output out of Git. Use English code comments and Chinese source product/error text with English catalogue entries for translated UI messages. For breaking changes, update the changelog with the migration path.

Report security vulnerabilities privately through the process in [SECURITY.md](SECURITY.md).
