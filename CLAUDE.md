# Montly

Self-hosted monthly recurring task tracker. Go+Chi+SQLite backend, React+Vite frontend, deployed via Docker.

## Stack
- **Backend:** Go 1.25, Chi router, modernc.org/sqlite (pure Go, no CGO)
- **Frontend:** React 19 + Vite, plain CSS, hooks pattern
- **Infra:** Multi-stage Dockerfile, docker-compose, Makefile

## Dev workflow
- `make up` — build image and start (always rebuilds)
- `make dev-backend` + `make dev-frontend` — local dev (two terminals; Vite proxies /api to :8080)
- `make setup` — first-time: go mod tidy + npm install
- `make test` — run all tests (Go + frontend Vitest)

## Key conventions
- All SQLite queries and migrations live in `backend/db.go`; migrations use idempotent ALTER TABLE
- All HTTP handlers in `backend/handlers.go` (tasks, completions, settings, receipts, CSV export); auth + token + setup + user handlers live in `backend/auth.go`; webhook handlers in `backend/webhooks.go`
- Frontend API calls are centralized in `frontend/src/api.js`
- Shared frontend utilities (e.g. `formatAmount`) live in `frontend/src/utils.js`
- Receipt files are uuid-named and stored in DATA_DIR/receipts/; unchecking a task removes the receipt AND the file
- `PUT /api/tasks/:id` backfills completion amounts: when a task's `metadata.amount` changes, past completions with no per-entry override have the old amount stamped onto them, preserving historical accuracy (`UpdateTaskWithAmountBackfill` in `db.go`)
- A mobile app will be added in the future — keep API design flexible

## First-run setup
On a fresh install with no users in the DB, the app serves a registration form (`SetupView.jsx`) instead of the login screen. The admin account is created via `POST /api/auth/setup`. `ADMIN_USERNAME` / `ADMIN_PASSWORD` env vars are still supported for automated/headless deployments but are no longer required.

## Key API endpoints
- `GET  /api/auth/setup` — `{"needs_setup": bool}`, public, no auth required
- `POST /api/auth/setup` — create first admin + open session; 409 if already set up
- `POST /api/auth/login` / `POST /api/auth/logout`
- `GET  /api/auth/me` — current user info
- `PATCH /api/auth/password` — change own password
- `GET  /api/tasks?month=YYYY-MM` / `POST /api/tasks` / `GET /api/tasks/:id` / `PUT /api/tasks/:id` / `DELETE /api/tasks/:id`
- `GET  /api/completions?month=YYYY-MM`
- `POST /api/completions/toggle` — toggle completion (body: `{task_id, month}`)
- `PATCH /api/completions/:task_id/:month` — update amount
- `POST /api/completions/:task_id/:month/receipt` / `DELETE /api/completions/:task_id/:month/receipt`
- `GET  /api/settings` / `PUT /api/settings`
- `GET  /api/auth/tokens` / `POST /api/auth/tokens` / `DELETE /api/auth/tokens/:id`
- `GET  /api/webhooks` / `POST /api/webhooks` / `DELETE /api/webhooks/:id`
- `GET  /api/export/completions.csv` — CSV export of completions
- `GET  /api/users` / `POST /api/users` / `DELETE /api/users/:id` — admin only

## Testing
- **Backend:** `cd backend && go test ./...` — covers auth, DB scoping, migrations, tokens, webhooks
- **Frontend:** `cd frontend && npm test` — Vitest + jsdom + Testing Library; tests live in `frontend/src/test/`
  - `App.test.jsx` — auth state machine, month nav, optimistic toggle, toasts, error handling
  - `TaskForm.test.jsx` — form rendering, validation, submit, cancel
  - `TaskList.test.jsx` — toggle, receipt confirm, PaymentSlot amount editing
  - `ManageView.test.jsx` — task list, search/type filters, edit/create/delete flow
  - `MonthPicker.test.jsx` — popover, inline, year nav, clear
  - `LoginView.test.jsx` — credentials, error display, loading state
  - `SetupView.test.jsx` — first-run form, validation, show/hide password, API error
  - `SettingsView.test.jsx` — settings form, token management, user management (admin)
  - `ReportView.test.jsx` — chart rendering, stat cards, loading and empty states
  - `api.test.js` — HTTP layer: status codes, error handling, request shape
  - `utils.test.js` — `formatAmount` en/eu number formats

## Available agents
Use these for focused reviews (invoke via subagent):
- **ui-ux** — layout, interaction design, feedback states, mobile readiness
- **security** — file upload safety, path traversal, SQL injection, HTTP headers
- **go-reviewer** — idiomatic Go, Chi patterns, SQLite correctness, error handling
- **accessibility** — WCAG 2.1 AA, ARIA, keyboard navigation, focus management
- **performance** — SQLite indexes, React re-renders, bundle size, Docker image
- **mobile** — touch targets, viewport behaviour, iOS/Android quirks, PWA readiness

## Slash commands
- `/review` — runs all six review agents in parallel and produces a single prioritised report
