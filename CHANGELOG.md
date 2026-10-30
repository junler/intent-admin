# Changelog

Notable changes are recorded here following Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Added

- JWT/Redis sessions, CAPTCHA, registration controls, effective permissions and personal access tokens.
- System management, monitoring, files, encrypted model/storage configuration and connection tests.
- Persistent changesets, conflict-aware rollback, inverse rollback and owner-bound confirmed plans.
- AI conversations, system knowledge, page context and MCP over HTTP/stdio.
- Named asynchronous handlers, scheduling, job logs and asynchronous Excel exports.
- Create-only XLSX imports for users, roles, parameters, dictionary data and demo items, including templates, row errors, downloadable reports, read-only previews and atomic confirmation. Files are limited to 200 data rows and 5 MB, further constrained by the changeset limit; owner-bound previews expire after 10 minutes.
- Cross-person plan approval with an assigned reviewer, private notifications, approve/reject/revoke/expiry states and retained decision history. Execution rechecks both participants' permissions, risk ceilings and the reviewed data.
- Selective changeset rollback with exact item selection, preview confirmation, indivisible custom compensation groups, per-item conflict fields and rollback links, partial parent status and reversible inverse changesets.
- An integrated queue console for queue/task summaries and details, retry, archive, cancellation, deletion, pause and resume. Controls enforce deployment namespaces, redact internal export details and preserve durable completion checks.
- MCP OAuth 2.1 authorization with discovery, public-client registration, PKCE S256, explicit consent, scoped access, rotating refresh tokens and revocation. PAT access remains supported.
- Permission-filtered MCP Resources for capabilities, dictionaries, entity knowledge, rules and the current principal, with authentication and permission checks on every listing/read.
- Chinese/English interface catalogues covering navigation, forms, previews, approval workflows, task controls and known server messages. The official Umi locale picker persists the choice and reloads the page.
- Container builds, private MinIO development storage, PostgreSQL/Redis integration suites and release archives.

### Changed

- The official Ant Design Pro / Umi Max interface now uses consistent cards, spacing, responsive tables, navigation and shared administration forms.
- Development and release frontend builds use npm with `web/package-lock.json`.
- Authentication replaces the Phase 0 development superuser actor.
- CI covers imports, cross-person approvals, partial/inverse rollback, OAuth SDK interoperability, private notifications, MCP Resources and queue controls, and checks translation coverage.

### Migration

Run `intent-admin migrate up` before starting this version. Migrations through `00009` add cross-person approvals, OAuth clients/grants/requests/tokens, private notifications, per-item rollback metadata and extension menu permissions. The schema contains 29 application tables, excluding migration metadata. New default menus are inserted without replacing administrator edits.

Configure `mcp.oauth_issuer` or `INTENT_ADMIN_MCP_OAUTH_ISSUER` with the canonical public origin for OAuth discovery and consent. The sample uses `http://127.0.0.1:8080`; production requires an HTTPS origin. An empty issuer disables OAuth without disabling PAT access. Additional approval and queue permissions must be granted to the intended roles; existing business permissions still apply.

Configure and test a model service before using AI. The initial administrator remains `admin` / `123456` only on a fresh installation; seed reruns preserve changed passwords. Back up both the encryption and JWT signing keys: credential-compensation vault records depend on the latter and expire with their retention period.

### Breaking changes

Frontend tooling moved from Vite/pnpm to Umi Max/npm. Use `npm ci`, `npm run dev` and `npm run build` in `web`. Unauthenticated Phase 0 API access is no longer accepted; sign in, use a scoped PAT, or authorize an OAuth client for MCP.
