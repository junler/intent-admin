# Changelog

Notable changes are recorded here following Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Added

- JWT/Redis sessions, CAPTCHA, registration controls, effective permissions and personal access tokens.
- System management, monitoring, files, encrypted model/storage configuration and connection tests.
- Persistent changesets, conflict-aware rollback, inverse rollback and owner-bound confirmed plans.
- AI conversations, system knowledge, page context and MCP over HTTP/stdio.
- Named asynchronous handlers, scheduling, job logs and asynchronous Excel exports.
- Official Ant Design Pro / Umi Max interface with dynamic menus and permission controls.
- Container builds, private MinIO development storage, CI integration suites and release archives.

### Changed

- Development and release frontend builds use npm with web/package-lock.json.
- Authentication replaces the Phase 0 development superuser actor.

### Migration

Run `intent-admin migrate up` before starting this version. Existing Phase 0/1 tables are upgraded incrementally. New default menus are inserted without replacing administrator edits. Configure and test a model service before using AI. The initial administrator remains `admin` / `123456` only on a fresh installation; seed reruns preserve changed passwords.

### Breaking changes

Frontend tooling moved from Vite/pnpm to Umi Max/npm. Use `npm ci`, `npm run dev` and `npm run build` in `web`. Unauthenticated Phase 0 API access is no longer accepted; sign in or use a scoped PAT for MCP.
