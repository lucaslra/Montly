# Montly

[![Website](https://img.shields.io/badge/website-lucaslra.github.io%2FMontly-4f7ef8)](https://lucaslra.github.io/Montly/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](docs/deployment.md)
[![Self-hosted](https://img.shields.io/badge/self--hosted-✓-4CAF50)](docs/deployment.md)

Self-hosted monthly recurring task tracker. Track bills, subscriptions, payments, and reminders — with receipt uploads, spending reports, multi-user support, and a clean mobile-friendly UI.

<table>
  <tr>
    <td><img src="docs/screenshots/tasks.png" alt="Task list" width="420"/></td>
    <td><img src="docs/screenshots/reports.png" alt="Reports" width="420"/></td>
  </tr>
  <tr>
    <td align="center"><em>Task list — monthly overview with progress and amounts</em></td>
    <td align="center"><em>Reports — spending history, forecast, and category breakdown</em></td>
  </tr>
</table>

## Features

- **Recurring tasks** — monthly, bi-monthly, quarterly, semi-annual, or annual intervals
- **Task types** — payment, subscription, bill, reminder (or none)
- **Completion tracking** — mark tasks done per month, attach receipt files (PDF, image)
- **Skip-a-month state** — explicitly skip a task for a given month, keeping it distinct from pending
- **Amount logging** — record the actual amount paid per completion; override the default per-month
- **Task archiving** — archive tasks instead of deleting; archived tasks are hidden from active views but preserve all completion history
- **Shared tasks** — share individual tasks with other users; shared users can toggle, skip, patch amounts, and upload receipts; sharing is managed per-task from the Manage view
- **Reports** — monthly spending bar chart with 6-month history and 3-month forecast, category donut chart, and stat cards (YTD/fiscal-year-to-date, monthly average, peak month, next forecast)
- **Webhooks** — outbound HTTP POST on task completion, uncompletion, skip, and monthly digest; testable directly from the Settings panel
- **Audit log** — append-only record of all completions, edits, deletes, user management, and token actions (admin only)
- **CSV import & export** — bulk-export all completions; import from the same format to migrate or load historical data
- **Multi-user** — per-user tasks, completions, and settings; admin can create/delete accounts; tasks can be shared across users
- **First-run setup** — create the admin account through the UI on first access; no env vars needed
- **API tokens** — headless / mobile client access via `Bearer mt_…` tokens
- **Settings** — per-user currency symbol, date format, color mode (light/dark/system), task sort order, completed-task position, fiscal year start month, number format (1,234.56 or 1.234,56)
- **MCP server** — optional sidecar that exposes Montly tasks as [MCP](https://modelcontextprotocol.io) tools for AI assistants (Claude Desktop, Cursor, etc.) — see [`mcp-server/README.md`](mcp-server/README.md)
- **Two databases** — SQLite (default, zero-config) or PostgreSQL
- **Self-contained** — single Docker image, no external services required for SQLite mode

## Quick start

### One-liner (try it out)

```bash
docker run -d \
  --name montly \
  -p 8080:8080 \
  -v montly_data:/data \
  -e SESSION_SECRET="$(openssl rand -base64 32)" \
  ghcr.io/lucaslra/montly:latest
```

Open `http://localhost:8080` — you'll be prompted to create the admin account on first access.

### Persistent install (recommended)

Create a `docker-compose.yml`:

```yaml
services:
  montly:
    image: ghcr.io/lucaslra/montly:latest
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - montly_data:/data
    environment:
      DATA_DIR: /data
      SESSION_SECRET: "replace-with-output-of-openssl-rand-base64-32"
      SECURE_COOKIES: "false"   # set to "true" when behind HTTPS
    restart: unless-stopped

volumes:
  montly_data:
```

Then:

```bash
docker compose up -d
```

> **HTTPS / production:** See [docs/deployment.md](docs/deployment.md) for reverse proxy setup (Caddy or nginx), PostgreSQL backend, backup procedures, and the full environment variable reference.

## Development

**Requires:** Go 1.25+, Node 18+.

Two-terminal workflow (Vite proxies `/api` to `:8080`):

```bash
make setup          # first time: go mod tidy + npm install
make dev-backend    # terminal 1 — Go API on :8080
make dev-frontend   # terminal 2 — Vite dev server on :5173
```

Run tests:

```bash
make test           # Go + frontend unit/integration tests
make e2e            # Playwright E2E tests (full stack in Docker, headless)
make e2e-headed     # Playwright E2E tests with a visible browser window
```

Or build and run the full stack via Docker:

```bash
make up
```

## Stack

| Layer    | Technology |
|----------|-----------|
| Backend  | Go 1.25, [Chi](https://github.com/go-chi/chi), [modernc/sqlite](https://gitlab.com/cznic/sqlite) (pure Go) or PostgreSQL |
| Frontend | React 19, Vite, plain CSS |
| Infra    | Multi-stage Docker, docker-compose, Makefile |

## API

All endpoints live under `/api` and `/api/v1` (both are equivalent). Authenticate with a session cookie (web UI) or an `Authorization: Bearer mt_<token>` header (API tokens).

```
POST   /api/auth/login
POST   /api/auth/logout
GET    /api/auth/setup                           — {"needs_setup": bool}, public
POST   /api/auth/setup                           — create first admin + open session
GET    /api/auth/me
PATCH  /api/auth/password

GET    /api/tasks?month=YYYY-MM
POST   /api/tasks
GET    /api/tasks/:id
PUT    /api/tasks/:id
DELETE /api/tasks/:id
PATCH  /api/tasks/:id/archive
PATCH  /api/tasks/:id/unarchive
GET    /api/tasks/archived
GET    /api/tasks/:id/shares
POST   /api/tasks/:id/shares
DELETE /api/tasks/:id/shares/:user_id

GET    /api/completions?month=YYYY-MM
POST   /api/completions/toggle
POST   /api/completions/skip
PATCH  /api/completions/:task_id/:month
POST   /api/completions/:task_id/:month/receipt
DELETE /api/completions/:task_id/:month/receipt

GET    /api/settings
PUT    /api/settings

GET    /api/auth/tokens
POST   /api/auth/tokens
DELETE /api/auth/tokens/:id

GET    /api/webhooks                             — events: task.completed, task.uncompleted, task.skipped, month.digest
POST   /api/webhooks
DELETE /api/webhooks/:id
POST   /api/webhooks/:id/test

GET    /api/report?anchor=YYYY-MM               — 6-month history + 3-month forecast in one request

GET    /api/export/completions.csv
POST   /api/import/completions.csv

GET    /api/users/lookup?q=                     — user search for share autocomplete
GET    /api/audit-logs                           — admin only
GET    /api/users                                — admin only
POST   /api/users                                — admin only
DELETE /api/users/:id                            — admin only
```

Every response includes `X-API-Version: 1`.

## Deployment

See **[docs/deployment.md](docs/deployment.md)** for:

- Environment variables reference
- Production docker-compose example
- Caddy / nginx reverse proxy setup
- PostgreSQL backend
- Backup procedures
- Security checklist

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions and guidelines.

To report a security vulnerability privately, use the process described in [SECURITY.md](SECURITY.md).

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md) code of conduct.

## License

MIT — see [LICENSE](LICENSE).
