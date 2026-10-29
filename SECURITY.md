# Security policy

## Supported versions

Security fixes target the latest released version and the current main branch. Earlier 0.x releases may require an upgrade; each release's changelog documents incompatible migrations.

## Report a vulnerability

Use GitHub's private vulnerability reporting form at https://github.com/junler/intent-admin/security/advisories/new when available. Include the affected version, required privileges, a minimal reproduction, impact and a proposed mitigation. Do not include live credentials or personal data.

If private reporting is unavailable, open an issue containing only a request for a private security contact. Wait for the maintainer's private channel before sending technical details. Do not publish an exploit in a public issue or pull request.

Maintainers aim to acknowledge reports within three business days, investigate promptly and agree on a coordinated disclosure date with the reporter. This is a community response target rather than a guaranteed support SLA.

## Deployment expectations

Replace development keys and credentials before production. Keep signing/encryption keys backed up separately from public configuration. Use HTTPS, narrow PAT scopes, Redis/PostgreSQL network controls and tested backups. Model and storage credentials belong in their dedicated configuration pages; their writes are closed to AI and MCP.
