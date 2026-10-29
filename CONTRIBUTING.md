# Contributing to Intent

Intent is an MIT-licensed, capability-driven administration system. Keep domain-specific applications outside the core repository. The implementation plan describes the product and architectural decisions; the current code and tests define the shipped API.

## Development environment

Install Go 1.26+, Node.js 22+, npm, PostgreSQL 17 and Redis 7. The frontend uses the official Ant Design Pro v6 scaffold, Umi Max and Pro Components. Its npm lockfile is authoritative.

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

## Validation

```sh
make test
make test-web
make lint
make build
```

Integration tests require disposable databases; some tests truncate tables. Never point them at development data that matters or production. CI creates isolated databases and runs all suites. To run the plan/rollback suite locally:

```sh
INTENT_ADMIN_TEST_DSN='host=127.0.0.1 port=5432 user=postgres password=test-only dbname=intent_plan_integration sslmode=disable' \
  go test ./internal/plan -v -count=1
```

Authentication, system policies, configuration/files and queues have separate opt-in integration flags documented in their test files and the CI workflow. Test permission failures, risk limits, before/after conflicts, transaction rollback and resource ownership when changing those behaviors. New low-impact presentation changes do not need tests that merely repeat the implementation.

## Pull requests

Use a feature branch and a Conventional Commit title such as `feat: add notice expiry` or `fix: reject stale rollback`. Include the problem, resulting behavior, validation and migration instructions. Add backward-compatible migrations; do not rewrite an already released migration. The schema has the 21 planned system tables, the demonstration table and a durable task-outbox infrastructure table. Keep secrets and generated build output out of Git. Use English code comments and Chinese product/error text. For breaking changes, update the changelog with the migration path.

Report security vulnerabilities privately through the process in [SECURITY.md](SECURITY.md).
