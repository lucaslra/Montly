# Montly

Self-hosted monthly recurring task tracker. Track bills, subscriptions, payments, and reminders — with receipt uploads, multi-user support, and a clean mobile-friendly UI.

## Features

- **Recurring tasks** — monthly, bi-monthly, quarterly, semi-annual, or annual intervals
- **Task types** — payment, subscription, bill, reminder (or none)
- **Completion tracking** — mark tasks done per month, attach receipt files (PDF, image)
- **Amount logging** — record the actual amount paid per completion
- **Multi-user** — isolated data per user; admin can create/delete accounts
- **API tokens** — headless / mobile client access via `Bearer mt_…` tokens
- **Settings** — per-user currency symbol, date format, color mode (light/dark/system)
- **Two databases** — SQLite (default, zero-config) or PostgreSQL
- **Self-contained** — single Docker image, no external services required for SQLite mode

## Quick start

```bash
cp .env.example .env
# Edit .env and set ADMIN_USERNAME and ADMIN_PASSWORD
docker compose up -d
```

Open `http://localhost:8080`.

> **HTTPS:** See [docs/deployment.md](docs/deployment.md) for reverse proxy setup (Caddy or nginx) and production configuration.

## Development

Two-terminal workflow (Vite proxies `/api` to `:8080`):

```bash
make setup          # first time: go mod tidy + npm install
make dev-backend    # terminal 1 — Go API on :8080
make dev-frontend   # terminal 2 — Vite dev server on :5173
```

Or build and run via Docker:

```bash
make up
```

## Stack

| Layer    | Technology |
|----------|-----------|
| Backend  | Go 1.25, [Chi](https://github.com/go-chi/chi), [modernc/sqlite](https://gitlab.com/cznic/sqlite) (pure Go) |
| Frontend | React 19, Vite, plain CSS |
| Infra    | Multi-stage Docker, docker-compose, Makefile |

## API

All endpoints live under `/api` and `/api/v1` (both are equivalent). Authenticate with a session cookie (web UI) or an `Authorization: Bearer mt_<token>` header (API tokens).

```
GET    /api/tasks?month=YYYY-MM
POST   /api/tasks
PUT    /api/tasks/:id
DELETE /api/tasks/:id

GET    /api/completions?month=YYYY-MM
POST   /api/completions/toggle
PATCH  /api/completions/:task_id/:month
POST   /api/completions/:task_id/:month/receipt
DELETE /api/completions/:task_id/:month/receipt

GET    /api/settings
PUT    /api/settings

GET    /api/auth/tokens
POST   /api/auth/tokens
DELETE /api/auth/tokens/:id
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

## License

MIT — see [LICENSE](LICENSE).
