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
- All HTTP handlers in `backend/handlers.go` (tasks, completions, settings, receipts); auth + token + setup handlers live in `backend/auth.go`
- Frontend API calls are centralized in `frontend/src/api.js`
- Shared frontend utilities (e.g. `formatAmount`) live in `frontend/src/utils.js`
- Receipt files are uuid-named and stored in DATA_DIR/receipts/; unchecking a task removes the receipt AND the file
- A mobile app will be added in the future — keep API design flexible

## First-run setup
On a fresh install with no users in the DB, the app serves a registration form (`SetupView.jsx`) instead of the login screen. The admin account is created via `POST /api/auth/setup`. `ADMIN_USERNAME` / `ADMIN_PASSWORD` env vars are still supported for automated/headless deployments but are no longer required.

## Key API endpoints
- `GET  /api/auth/setup` — `{"needs_setup": bool}`, public, no auth required
- `POST /api/auth/setup` — create first admin + open session; 409 if already set up
- `POST /api/auth/login` / `POST /api/auth/logout`
- `GET  /api/tasks?month=YYYY-MM` / `POST /api/tasks` / `PUT /api/tasks/:id` / `DELETE /api/tasks/:id`
- `GET  /api/completions?month=YYYY-MM` / `POST /api/completions/:task_id/:month`
- `GET  /api/settings` / `PUT /api/settings`

## Testing
- **Backend:** `cd backend && go test ./...` — covers auth, DB scoping, migrations, tokens
- **Frontend:** `cd frontend && npm test` — Vitest + jsdom + Testing Library; tests live in `frontend/src/test/`
  - `TaskForm.test.jsx` — form rendering, validation, submit, cancel
  - `MonthPicker.test.jsx` — popover, inline, year nav, clear
  - `TaskList.test.jsx` — toggle, receipt confirm, PaymentSlot amount editing
  - `LoginView.test.jsx` — credentials, error display, loading state
  - `api.test.js` — HTTP layer: status codes, error handling, request shape

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
