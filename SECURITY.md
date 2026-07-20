# Security Policy

## Supported versions

The `main` branch and the latest tagged release receive security fixes.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

- Prefer [GitHub Security Advisories](https://github.com/datehoer/subpool/security/advisories/new) if available
- Or contact the repository owner privately

Include steps to reproduce, impact, and (if possible) a suggested fix. Do **not** attach real subscription URLs, tokens, or node credentials.

## Operational notes

- Keep subscription credentials only in local config (e.g. `subpool.conf`); it is gitignored by default
- Do not bind the stats HTTP endpoint to public interfaces without access control
