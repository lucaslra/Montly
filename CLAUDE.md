# Montly

Self-hosted monthly recurring task tracker. Go+Chi+SQLite backend, React+Vite frontend, deployed via Docker.

## Stack
- **Backend:** Go 1.25, Chi router, modernc.org/sqlite (pure Go, no CGO)
- **Frontend:** React 19 + Vite, plain CSS, hooks pattern
- **Infra:** Multi-stage Dockerfile, docker-compose, Makefile

## Dev workflow
- `make up` — build image and start (always rebuilds)
- `make dev-backend` + `make dev-frontend` — local dev (two terminals; Vite proxies /api to :8080)
- `make setup` — first-time: go mod tidy + npm install (backend, frontend, and e2e)
- `make test` — run all tests (Go + frontend Vitest)
- `make e2e` — run Playwright E2E tests fully in Docker (ephemeral DB, headless)
- `make e2e-headed` — run E2E tests with a visible browser; app in Docker, Playwright runs locally (requires `cd e2e && npx playwright install chromium` once)

## Key conventions
- All SQLite queries and migrations live in `backend/db.go`; migrations use idempotent ALTER TABLE; includes `task_shares` join table for per-task collaborators
- All HTTP handlers in `backend/handlers.go` (tasks, completions, settings, receipts, CSV export); auth + token + setup + user handlers live in `backend/auth.go`; webhook handlers in `backend/webhooks.go`; OpenID Connect (SSO) config, provider, and handlers live in `backend/oidc.go`
- OIDC is optional (enabled when `OIDC_ISSUER` is set) and layers on top of the existing session: after the auth-code+PKCE flow completes, the callback calls the same `setSession()` as password login, so downstream auth is unchanged. Handlers depend on an `OIDCProvider` interface (real impl wraps `coreos/go-oidc`) so they unit-test against a fake IdP. State/nonce/PKCE-verifier ride in a short-lived HMAC-signed `_montly_oidc` cookie (SameSite=Lax). User resolution order: linked `sub` → verified-email link → username link → JIT provision; first-ever SSO user is admin; `OIDC_ADMIN_GROUP` syncs admin on every login. `users` gained nullable `email`/`oidc_issuer`/`oidc_subject` columns + a partial unique index
- Webhook delivery uses `safeWebhookClient()` which resolves DNS and rejects private/loopback/link-local IPs (covers cloud metadata endpoints like 169.254.169.254) before connecting; redirects are re-validated at each hop and capped at 3
- Task access uses two helpers: `taskOwnerCheck` (owner-only actions like edit/archive/share management) and `taskAccessCheck` (owner OR shared user — used for completions, receipt upload/delete, skip)
- Frontend API calls are centralized in `frontend/src/api.js`
- Shared frontend utilities (e.g. `formatAmount`) live in `frontend/src/utils.js`
- Receipt files are uuid-named and stored in DATA_DIR/receipts/; unchecking a task removes the receipt AND the file; receipts are accessible to the task owner and any shared users
- `PUT /api/tasks/:id` backfills completion amounts: when a task's `metadata.amount` changes, past completions with no per-entry override have the old amount stamped onto them, preserving historical accuracy (`UpdateTaskWithAmountBackfill` in `db.go`)
- Webhooks fire against the **task owner's** subscriptions, not the acting user's — matters when a shared user toggles a task
- A mobile app will be added in the future — keep API design flexible
- MCP server lives in `mcp-server/` — a separate Go module with its own `Dockerfile`; it wraps the Montly REST API as MCP tools for AI assistants (Claude Desktop, Cursor, etc.)
- MCP server code is split into `client.go` (HTTP client + API types), `tools.go` (tool handlers + enrichment), `main.go` (server + transport wiring)
- MCP server runs as a separate Docker container (`ghcr.io/lucaslra/montly-mcp`); uses `MONTLY_URL` + `MONTLY_TOKEN` to talk to the main app, serves MCP over stdio (default) or Streamable HTTP when `MCP_PORT` is set
- MCP tools: `list_tasks` (read tasks+completions for a month), `get_report` (6-month history + 3-month forecast), `toggle_task` (mark done/undo), `skip_task` (skip/unskip), `update_completion` (set paid amount or note), `create_task` (create recurring task)

## First-run setup
On a fresh install with no users in the DB, the app serves a registration form (`SetupView.jsx`) instead of the login screen. The admin account is created via `POST /api/auth/setup`. `ADMIN_USERNAME` / `ADMIN_PASSWORD` env vars are still supported for automated/headless deployments but are no longer required. Passwords must be 8–72 characters (bcrypt truncates silently at 72). When OIDC is enabled, `SetupView.jsx` and `LoginView.jsx` also render a "Sign in with SSO" button (fed by `GET /api/auth/config`), and the first SSO user bootstraps as admin.

## Key API endpoints
- `GET  /api/auth/config` — `{"password_login":bool,"oidc":{"enabled":bool,"provider_name":str}}`, public
- `GET  /api/auth/setup` — `{"needs_setup": bool}`, public, no auth required
- `POST /api/auth/setup` — create first admin + open session; 409 if already set up
- `GET  /api/auth/oidc/login` — start the OIDC auth-code flow (302 to IdP), public
- `GET  /api/auth/oidc/callback` — OIDC redirect target; verifies state/nonce, links/provisions user, opens session, 302 to `/` (errors 302 to `/?auth_error=`), public
- `POST /api/auth/login` / `POST /api/auth/logout`
- `GET  /api/auth/me` — current user info
- `PATCH /api/auth/password` — change own password
- `GET  /api/tasks?month=YYYY-MM` / `POST /api/tasks` / `GET /api/tasks/:id` / `PUT /api/tasks/:id` / `DELETE /api/tasks/:id`
- `PATCH /api/tasks/:id/archive` / `PATCH /api/tasks/:id/unarchive` / `GET /api/tasks/archived`
- `GET  /api/tasks/:id/shares` / `POST /api/tasks/:id/shares` / `DELETE /api/tasks/:id/shares/:user_id` — owner only
- `GET  /api/completions?month=YYYY-MM`
- `POST /api/completions/toggle` — toggle completion (body: `{task_id, month}`)
- `POST /api/completions/skip` — skip/unskip for this month (body: `{task_id, month}`)
- `PATCH /api/completions/:task_id/:month` — update amount or note
- `POST /api/completions/:task_id/:month/receipt` / `DELETE /api/completions/:task_id/:month/receipt`
- `GET  /api/settings` / `PUT /api/settings`
- `GET  /api/auth/tokens` / `POST /api/auth/tokens` / `DELETE /api/auth/tokens/:id`
- `GET  /api/webhooks` / `POST /api/webhooks` / `DELETE /api/webhooks/:id` / `POST /api/webhooks/:id/test`
- `GET  /api/report?anchor=YYYY-MM` — 6-month history + 3-month forecast in one response
- `GET  /api/export/completions.csv` — CSV export of completions
- `POST /api/import/completions.csv` — bulk import completions from CSV
- `GET  /api/users/lookup?q=` — user search for share autocomplete (excludes self, max 20)
- `GET  /api/users` / `POST /api/users` / `DELETE /api/users/:id` — admin only
- `GET  /api/audit-logs?limit=&offset=` — paginated audit log, admin only

## Testing
- **Backend:** `cd backend && go test ./...` — covers auth, DB scoping, migrations, tokens, webhooks, archive/unarchive, skip, receipt upload/delete, task shares, shared-task access, webhook actor logic; test files:
  - `db_test.go` — DB-layer unit tests: task CRUD, archive lifecycle, audit log pagination, share CRUD, shared-task visibility, user lookup
  - `handlers_test.go` — HTTP handler integration tests: all endpoints exercised via in-memory router + SQLite
  - `oidc_test.go` — OIDC unit tests: config parsing/validation, signed state-cookie roundtrip/tamper/expiry, claim extraction, `resolveOIDCUser` (JIT first-user-admin, link by verified email/username, unverified-email guard, username dedupe, signup disabled, admin-group sync), and the login/callback/config handlers via a fake `OIDCProvider`
- **Frontend:** `cd frontend && npm test` — Vitest + jsdom + Testing Library; tests live in `frontend/src/test/`
  - `App.test.jsx` — auth state machine, month nav, optimistic toggle, toasts, error handling
  - `TaskForm.test.jsx` — form rendering, validation, submit, cancel
  - `TaskList.test.jsx` — toggle, receipt confirm, PaymentSlot amount editing, shared-by badge
  - `ManageView.test.jsx` — task list, search/type filters, edit/create/delete/archive flow, SharePanel (loading/empty/populated, add/remove, error, close)
  - `MonthPicker.test.jsx` — popover, inline, year nav, clear
  - `LoginView.test.jsx` — credentials, error display, loading state
  - `SetupView.test.jsx` — first-run form, validation, show/hide password, API error
  - `SettingsView.test.jsx` — settings form, token management, user management (admin)
  - `ReportView.test.jsx` — chart rendering, stat cards, loading and empty states
  - `api.test.js` — HTTP layer: status codes, error handling, request shape; covers all API functions including archive, shares, lookup, import
  - `utils.test.js` — `formatAmount` en/eu number formats
- **MCP server:** `cd mcp-server && go test ./...` — covers the HTTP client, all 6 tool handlers, input validation, and API error propagation
  - `client_test.go` — auth header, URL construction, error handling, constructor defaults, POST/PATCH methods, month encoding/validation
  - `tools_test.go` — list_tasks (pending/mixed/override/receipt/shared/note/empty/default-month/invalid-month/API-errors), get_report (summary/skipped-exclusion/API-error), toggle_task (complete/uncomplete/default-month/missing-id/invalid-month/API-error), skip_task (skip/unskip/missing-id/API-error), update_completion (amount/note/missing-fields/missing-id/API-error), create_task (payload/title-required/date-validation/empty-fields/API-error), resolveMonth
  - `main_test.go` — server creation
- **E2E:** `make e2e` — Playwright 1.52 against the full Docker stack; 90 tests across 6 suites:
  - `01-auth.spec.ts` — setup flow, login/logout, protected routes, token auth
  - `02-tasks.spec.ts` — create, edit, delete, search, CSV import
  - `03-completions.spec.ts` — toggle, amount editing, receipt attach/remove, notes, skip, cross-month isolation
  - `04-settings.spec.ts` — preferences, password change, API tokens, webhooks, user management, audit log
  - `05-sharing.spec.ts` — shared task lists, collaborator add/remove, archive flows
  - `06-oidc.spec.ts` — SSO button, full auth-code+PKCE flow sign-in, session persistence, callback error handling; the E2E stack adds a `mock-oidc` service (`e2e/mock-oidc/server.mjs` — a zero-dependency Node mock IdP: discovery, JWKS, /authorize, RS256 /token) and wires the app to it via `OIDC_*` env in `docker-compose.e2e.yml`. Both the app and the Playwright browser share the Docker network, so one issuer hostname (`mock-oidc:9000`) works for both
  - `e2e/global-setup.ts` runs once to create the admin account and persist the session; `e2e/fixtures/` holds runtime-generated test files (gitignored)
  - Note: three specs (`02-tasks` delete-reminder, `04-settings` change-password-present, `05-sharing` create-sharee) fail on `main` too — pre-existing, unrelated to OIDC

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
