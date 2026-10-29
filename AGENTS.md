# Repository guidance

- Product name: Intent; repository, Go module, binary and image identifier: intent-admin.
- Backend: Go 1.26, Gin, GORM/PostgreSQL, Redis, Goose and Asynq. Frontend: official Ant Design Pro v6 scaffold with Umi Max, React, Ant Design and Pro Components. Use npm and web/package-lock.json.
- Preserve the single capability execution path for HTTP, AI and MCP. Permissions, risk, previews, audit and changesets belong there.
- Join ambient transactions through dbx.DBFrom. Queue cache invalidation with dbx.AfterCommit. Preview code must be read-only.
- Record changed fields explicitly. Never put credentials, their ciphertext or token hashes in logs, previews or changeset snapshots; use the existing encrypted credential/vault mechanism.
- Keep model/storage configuration writes, personal password changes and token creation closed to AI/MCP.
- Use reviewable incremental SQL migrations. Keep old migration versions stable and make seed reruns preserve operator changes.
- Follow current UI patterns, dynamic authorized menus and shared permission controls. Product/error text is Chinese; source comments and contributor documentation are English.
- Read CONTRIBUTING.md for the complete Capability example and test commands. Run focused checks while implementing, then go test ./..., go vet ./..., frontend tests/type checking and the production build when relevant.
- Integration suites require disposable databases and opt-in flags; see .github/workflows/ci.yml. Keep secrets, local config and generated artifacts out of commits.
