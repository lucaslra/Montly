# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, use [GitHub's private security advisory](https://github.com/lucaslra/montly/security/advisories/new) to report the issue. You'll receive a response within a few days. If a fix is warranted, a patch will be released and you'll be credited (unless you prefer otherwise).

## Supported Versions

Only the latest release receives security fixes. Running a recent image is strongly recommended.

## Security Design Notes

A few properties of the current design that are relevant when assessing impact:

- **Self-hosted only** — no central server; each deployment is isolated and operator-controlled.
- **Session cookies** — `HttpOnly`, `SameSite=Lax`; set `SECURE_COOKIES=true` when serving over HTTPS (the default in production).
- **Passwords** — stored as bcrypt hashes (cost 12).
- **API tokens** — `mt_` prefixed, stored as SHA-256 hashes; never returned after creation.
- **File uploads** — receipts are UUID-named, stored outside the web root, and served only to the owning user.
- **SQLite** — WAL mode, foreign keys enforced; parameterised queries throughout.
- **Rate limiting** — login endpoint is rate-limited per IP.
- **Docker** — image runs as a non-root user (UID 1000).
