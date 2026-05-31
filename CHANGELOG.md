# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [0.22.0] — 2026-05-31

### Added
- **Click-anywhere task toggle** — clicking anywhere on the task row now toggles completion; added `aria-label` attributes to icon buttons for screen readers.

### Changed
- **Lighter dark mode** — dark theme backgrounds, surfaces, borders, badge colors, and hover-state overrides bumped ~4 shades lighter for improved readability.

### Fixed
- **MCP schema descriptions** — removed spurious `required,` prefix from JSON Schema tool descriptions.

## [0.21.0] — 2026-05-31

### Added
- **MCP tools** — 5 new tools: `get_report` (spending history + forecast), `toggle_task` (mark done/undo), `skip_task`, `update_completion` (set paid amount or note), `create_task`; the server now covers the full daily workflow.
- **MCP client configs** — setup instructions for Claude Desktop (stdio + HTTP), Claude Code, and Cursor.
- **MCP user guide** — README rewritten with quickstart, natural-language usage examples for all 6 tools, and a limitations section.

### Fixed
- **MCP report skip bug** — `get_report` was including skipped tasks in `total_due`, inflating the amount vs what the web UI shows.
- **MCP `parseFloat`** — replaced `fmt.Sscanf` (silently swallowed errors) with `strconv.ParseFloat`.
- **MCP shutdown** — `srv.Shutdown` error is now logged instead of silently dropped.

### Changed
- **MCP client hardened** — unified `do()` method with POST/PATCH support, month validation via regex, URL encoding on all params, 10 MB response body cap. Test count: 20 → 51.

## [0.20.0] — 2026-05-30

### Fixed
- **Webhook `task.skipped` event** — subscribers now receive real deliveries when a task is skipped (was subscribed but never fired).
- **`AddTaskShare` Content-Type** — response now correctly sets `Content-Type: application/json` (was sniffed as `text/plain` due to `WriteHeader` ordering).
- **Amount validation** — `NaN`, `Infinity`, and negative values are now rejected in both task creation and completion patching; previously they passed validation and could corrupt spending totals.
- **Skip error matching** — replaced fragile string comparison with a sentinel `ErrAlreadyCompleted` error.
- **Progress bar with skipped tasks** — skipped tasks are now excluded from the denominator so 100% is reachable; the monetary summary also excludes skipped payments from the "due" total.
- **Task checkbox tap target** — expanded from 20×20px to 44×44px minimum, meeting mobile touch target guidelines.

### Changed
- **Static asset caching** — Vite-built assets under `/assets/` now return `Cache-Control: public, max-age=31536000, immutable`, eliminating redundant downloads on repeat visits.
- **Documentation** — 16 fixes across README, CLAUDE.md, SECURITY.md, deployment guide, CONTRIBUTING, MCP server README, PR template, and issue templates.

## [0.19.0] — 2026-05-30

### Added
- **MCP server** — standalone MCP server (`mcp-server/`) exposes Montly tasks as tools for AI assistants (Claude Desktop, Cursor, etc.); runs as a separate Docker container with its own Dockerfile and test suite.
- **Skip-to-content link** — keyboard and screen-reader users can now bypass the header navigation (WCAG 2.4.1).

### Security
- **Session validation on deleted users** — session cookies now verify the user still exists in the database on every request; deleted users are immediately locked out instead of retaining access until cookie expiry.

## [0.18.0] — 2026-05-02

### Added
- **Dependabot** — automated weekly dependency updates for Go modules, npm packages, and GitHub Actions.
- **GitHub issue and PR templates** — bug report, feature request, and pull request templates added under `.github/`.

### Changed
- **README** — website badge added; simplified Docker one-liner and persistent-install compose snippet in Quick Start.

### Dependencies
- Vite v6 → v8, `@vitejs/plugin-react` v4 → v6 (combined — v6 requires Vite ≥ 8)
- `modernc.org/sqlite` v1.33.1 → v1.50.0; `go-chi/chi` v5.1.0 → v5.2.5
- `jsdom` v29.0.2 → v29.1.1
- GitHub Actions: `actions/checkout` v4 → v6, `setup-go` v5 → v6, `setup-node` v4 → v6, `docker/setup-buildx-action` v3 → v4, `docker/login-action` v3 → v4

## [0.17.0] — 2026-05-02

### Security
- **Request body size limits** — `parseTaskBody`, `CreateWebhook`, and `CreateUser` now cap request bodies before decoding (16 KB / 4 KB respectively), preventing memory exhaustion via oversized payloads
- **bcrypt password length cap** — passwords are now rejected above 72 characters at all entry points (setup, create user, change password); bcrypt silently truncates at byte 72, which would allow any input sharing the first 72 bytes to authenticate as the same account
- **SSRF protection for webhooks** — webhook delivery resolves hostnames before connecting and rejects any IP in loopback, RFC 1918, link-local (covers cloud metadata endpoints like 169.254.169.254), carrier-grade NAT, and IPv6 unique-local ranges; redirects are re-validated at each hop and capped at 3

### Added
- **Migration step logging** — startup now logs each schema change as it runs, or confirms "migration: schema up to date" when nothing needs applying

## [0.16.0] — 2026-05-02

### Added
- **Amount as first-class column** — `tasks.amount` is now a dedicated column instead of being embedded in `metadata` JSON. `PUT /api/tasks/:id` backfills historical completions automatically: when a task's default amount changes, past completions with no per-entry override have the old amount stamped onto them, preserving historical accuracy.

### Changed
- Test suite significantly expanded — 9 new backend test PRs covering report pipeline, webhook handler, `FireMonthDigest`, audit log handler, receipt serving, settings validation, `CompleteSkipped` toggle path, TaskList, App.jsx mutations; backend handler test count roughly doubled

### Fixed
- Removed dead code: `db.UpdateTask` (superseded by `UpdateTaskWithAmountBackfill`), collapsed identical `LookupUsers` driver branch
- Fixed 5 CSS rules using undefined `var(--accent)` CSS variable — replaced with `var(--primary)`

### Tests
- Added E2E spec `05-sharing.spec.ts` — 14 tests covering shared task list visibility, collaborator add/remove, shared toggle, and archive flows; total E2E suite now 86 tests across 5 specs

## [0.15.0] — 2026-04-30

### Added
- **Shared task lists** — task owners can share individual tasks with other users. Shared users see the task in their monthly view (with a "shared by X" badge), can toggle, skip, patch, and upload receipts. Sharing is managed from the Manage view via an inline Share panel with live user search. Completions and receipts on shared tasks are shared state — one completion is visible to all collaborators.
- `GET/POST/DELETE /api/tasks/{id}/shares` — manage per-task collaborators; only the task owner can add/remove; self-share returns 400.
- `GET /api/users/lookup?q=` — user search endpoint for the share autocomplete (returns up to 20 matches, excludes the requesting user).

### Fixed
- **Webhook actor** — `task.completed` and `task.uncompleted` webhooks now fire against the task owner's webhook subscriptions, not the actor's (matters when a shared user toggles a task).

## [0.14.0] — 2026-04-30

### Fixed
- **EU decimal separator in task amount field** — when the number format is set to EU (`1.234,56`), the Add/Edit Task dialog now accepts `,` as the decimal separator, shows `0,00` as placeholder, and normalises the value to a plain float before saving. Existing stored amounts are displayed with the correct separator when reopening the edit form.

## [0.13.0] — 2026-04-25

### Added
- **Task archiving** — tasks can now be archived instead of hard-deleted. Archived tasks vanish from the active list and all month views; a collapsible "Archived tasks" section in Manage lets users restore them or delete permanently. Completion history and report chart data are preserved for archived tasks across all history months.

### Changed
- **Manage view delete flow** — the "Delete" button on active tasks is now "Archive" (with confirmation). Permanent deletion is only available for already-archived tasks.

## [0.12.0] — 2026-04-25

### Added
- **Webhook test button** — each webhook in Settings → Webhooks now has a "Test" button that fires an immediate synchronous delivery using real payload format (task event or digest, depending on subscribed events). The result (`Test delivered HTTP 200` or `Test failed: …`) is shown inline without leaving the page.
- **`GET /api/report` endpoint** — single endpoint replaces 15 parallel-but-serialized per-month requests. Returns 6 months of history + 3 months of forecast in one response using two DB queries; frontend caches by anchor month.

### Fixed
- **Audit log pagination** — navigating between pages hid the Prev/Next controls because the logs array was cleared to `null` mid-transition. Controls now stay visible while the next page loads; buttons are disabled during in-flight requests instead of disappearing.
- **Report page load time** — SQLite's single writer connection caused all parallel per-month requests to serialize server-side, producing 2–4 s loads even with little data. The new `/api/report` endpoint eliminates the round-trips entirely.

## [0.11.0] — 2026-04-24

### Added
- **Settings tab deep-links** — each Settings tab now has a unique URL (`/settings#preferences`, `/settings#tokens`, etc.). The active tab is reflected in the hash on switch and restored on load, so links and back-navigation land on the right tab.

### Fixed
- **CSP inline script error (residual)** — added an explicit `script-src 'self'` directive and whitelisted the remaining blocked inline-script hash, covering builds served from a cached Docker layer that pre-dates the `modulepreload` polyfill fix.

## [0.10.0] — 2026-04-24

### Added
- **Monthly digest webhook** — new `month.digest` event fires at 08:00 UTC on the 1st of every month. Payload includes the full task list for that month with types and amounts, plus a `total_amount` sum. Subscribe per-webhook in Settings → Webhooks.

### Fixed
- **CSP inline script error** — disabled Vite's `modulepreload` polyfill, which was injecting an inline script blocked by `default-src 'self'`. Native modulepreload support is effectively universal; the polyfill is unnecessary.

## [0.9.0] — 2026-04-24

### Changed
- **Settings tabs** — the Settings page is now split into six tabs (Preferences, Password, API Tokens, Webhooks, Users, Audit), eliminating the long single-page scroll. Users and Audit tabs are only visible to admins.

## [0.8.0] — 2026-04-24

### Changed
- **Landing page** — added favicon (blue rounded "M"), version badge in the hero linking to the current release, `docker pull ghcr.io/lucaslra/montly:latest` as an alternative install path, and full social preview tags (`og:image`, `og:url`, `og:type`, image dimensions, `twitter:card`) so WhatsApp, Slack, and iMessage generate rich link previews.

## [0.7.0] — 2026-04-24

### Added
- **Playwright E2E suite** — 72 tests across 4 serial specs (auth, tasks, completions, settings) run against the full Docker stack with an ephemeral database. `make e2e` runs headless in Docker; `make e2e-headed` runs with a visible browser (app in Docker, Playwright local). `make setup` now also installs E2E dependencies.

### Fixed
- **CSP `upgrade-insecure-requests` on HTTP deployments** — the directive was set unconditionally, causing Chromium to silently upgrade all same-origin HTTP asset requests to HTTPS for non-`localhost` origins (e.g. the Docker service hostname `app`), which prevented the JS bundle from loading and broke the login flow. The directive is now only included when `SECURE_COOKIES=true`.

## [0.6.0] — 2026-04-24

### Added
- **CSV import** — `POST /api/import/completions.csv` accepts the same format produced by the CSV export (`Title,Type,Month,Status,Amount,Has Receipt`). Tasks are matched by title + type; a minimal placeholder task is created when no match is found. Completions are inserted or updated atomically — existing receipts and notes are never touched. The `Has Receipt` column is accepted but ignored. Returns `{"tasks_created":N,"completions_created":N,"completions_updated":N}`. An Import section now appears below Export in the Reports view.

## [0.5.0] — 2026-04-24

### Added
- **Amount backfill on price change** — updating a task's default amount now stamps the previous amount onto past completions that had no per-entry override, preserving historical accuracy without any schema changes.

### Fixed
- **Month picker squeezed in jump mode** — the inline panel was overriding `width: auto`, collapsing the month grid to text-only width; restored to the standard 220 px.
- **Multi-arch Docker builds** — builder stages now use `--platform=$BUILDPLATFORM` and cross-compile via `GOOS`/`GOARCH`, removing slow QEMU emulation.

## [0.4.1] — 2026-04-24

### Fixed
- **Settings not saving** — `task_sort`, `completed_last`, `fiscal_year_start`, and `number_format` were silently dropped by `UpdateSettings`; changes appeared to save but reverted immediately on the next response
- **`fiscal_year_start` normalisation** — leading-zero values (e.g. `"007"`) now stored as the canonical integer string (`"7"`)
- **Unbounded `UpdateSettings` body** — added `MaxBytesReader` (4 KB cap) to match every other JSON-body handler; previously any client could stream an arbitrarily large body
- **Settings save error swallowed** — `handleSaveSettings` caught API errors without re-throwing, making the in-form error banner in `SettingsView` unreachable; errors now propagate correctly

### Accessibility
- Token reveal box now carries `role="alert"` so screen readers announce the one-time token immediately on creation (WCAG 4.1.3)
- Webhook events checkbox group now uses `role="group"` + `aria-labelledby` so the group label is programmatically associated (WCAG 1.3.1, 4.1.2)
- Confirm dialogs (Revoke token, Delete user, Delete webhook) now manage focus: the **Yes** button receives focus when the dialog opens; focus returns to the trigger when dismissed with **No** (WCAG 2.4.3)

## [0.4.0] — 2026-04-24

### Added
- **Skip-a-month state** — tasks can now be explicitly skipped so intentionally-skipped months are distinguishable from pending ones. Skip/un-skip buttons appear inline in the task list; toggling a skipped task marks it complete instead of removing the skip.
- **`task.skipped` webhook event** — fired when a task is skipped, alongside the existing `task.completed` / `task.uncompleted` events.
- **Audit log** — append-only `audit_logs` table records who completed, edited, deleted, or skipped tasks, and who managed users, tokens, webhooks, and passwords. Surfaced as a paginated table in admin Settings.
- **CSV export includes `Status` column** — values: `completed` or `skipped`.

### Changed
- CI push trigger restricted to `main`; paths filter on both push and pull_request so doc-only changes don't burn CI minutes; concurrency group cancels stale in-progress runs.

## [0.3.2] — 2026-04-23

### Added
- `SECURITY.md` — vulnerability disclosure process and security design notes
- `CONTRIBUTING.md` — setup instructions, code conventions, and PR guidelines
- `CODE_OF_CONDUCT.md` — Contributor Covenant v2.1
- README badges: Go version, React version, Docker, self-hosted, MIT license
- README preview screenshots: desktop task list, reports view, mobile layout

## [0.3.1] — 2026-04-23

### Added
- Expanded frontend test suite from 63 tests (5 files) to 189 tests (11 files)
  - `App.test.jsx` — auth state machine, month navigation, optimistic toggle, toast notifications, error handling
  - `SettingsView.test.jsx` — settings form, API token management, user management (admin)
  - `ReportView.test.jsx` — chart rendering, stat cards, loading/empty states
  - `ManageView.test.jsx` — task list rendering, search/type filters, edit/create/delete flows
  - `SetupView.test.jsx` — first-run registration form, password validation, show/hide toggle, API errors
  - `utils.test.js` — `formatAmount` for en and eu number formats
  - `api.test.js` — expanded coverage: all API functions, AbortSignal, error propagation

## [0.3.0] — 2026-04-23

### Added
- **Reports page** — accessible via the 📊 button in the header
  - Monthly spending bar chart: 6 months history, current month, 3-month forecast
  - Expected-amount tick marks and hover tooltip on each bar
  - Category donut chart: payment / subscription / bill breakdown for the last 3 months
  - Stat cards: YTD (or fiscal-year-to-date), monthly average, peak month, next-month forecast
- **First-run UI setup** — on a fresh install with no users, the app shows a registration form instead of a login screen; admin account is created directly in the browser
  - `GET /api/auth/setup` — public endpoint returning `{"needs_setup": bool}`
  - `POST /api/auth/setup` — creates first admin and opens a session; returns 409 if already set up
  - `ADMIN_USERNAME` / `ADMIN_PASSWORD` env-var bootstrap still supported for automated deployments
- **4 new per-user settings**
  - Task order: by type (payment → subscription → bill → reminder), alphabetical, or creation order
  - Completed tasks: mixed in (default) or pushed to the bottom of the list
  - Fiscal year start month: shifts the YTD stat card label and value in reports
  - Number format: `1,234.56` (English) or `1.234,56` (European)
- `frontend/src/utils.js` — shared `formatAmount(amount, currency, numberFormat, decimals)` utility used across monetary displays

### Changed
- Default currency changed from `$` to `€` for new installs
- `make up` now always rebuilds the Docker image (`--build` flag)
- Settings preview now shows a formatted amount reflecting the selected number format

## [0.2.0] — 2026-04-22

### Added
- README with feature list, quick-start, API overview, and deployment links
- LICENSE (MIT)
- Frontend test suite: 63 tests with Vitest + jsdom + Testing Library
  - `TaskForm` — rendering, amount/date validation, submit, cancel, Escape
  - `MonthPicker` — popover, inline, year navigation, clear
  - `TaskList` — toggle, receipt confirm dialog, PaymentSlot amount editing
  - `LoginView` — credentials, error display, loading state
  - `api.js` — HTTP status handling, request shape, AbortSignal
- GitHub Actions CI pipeline — backend tests, frontend tests, Docker build (on every push)
- GitHub Actions release pipeline — builds and pushes Docker image to ghcr.io, creates GitHub release (on `v*.*.*` tags)
- `make test` — runs both backend and frontend test suites

### Fixed
- `backend/dist/index.html` placeholder now tracked in git so `go:embed dist` works in CI and fresh clones
- `.gitignore` pattern changed from `backend/dist/` to `backend/dist/*` to allow the `!backend/dist/index.html` negation to take effect

### Changed
- Go version bumped to 1.25 across all documentation and agent definitions
- `/review` command updated to launch all six specialist agents (added mobile)
- `TRUST_PROXY_HEADERS` added to the deployment guide environment variable table

## [0.1.0] — 2026-04-22

Initial release.

### Added
- Monthly recurring task tracker with configurable intervals (1, 2, 3, 6, 12 months)
- Task types: payment, subscription, bill, reminder
- Per-month completion tracking with optional amount and receipt file attachment
- Receipt uploads: PDF, JPG, PNG, WebP, GIF — validated by magic bytes, stored as UUIDs
- Multi-user support with admin role; admin can create and delete accounts
- API token authentication (`mt_` prefixed) for headless/mobile clients
- Per-user settings: currency symbol, date format, color mode (light/dark/system)
- SQLite (default) and PostgreSQL backends
- Session-cookie auth with HMAC signing; rate-limited login endpoint
- Security headers: CSP, X-Frame-Options, Referrer-Policy, HSTS (when SECURE_COOKIES=true)
- Single-binary Docker image with embedded frontend (go:embed)

### Fixed
- Correct HTTP 201 status on resource creation endpoints
- Idempotent ALTER TABLE migrations safe to re-run on startup
- Task delete now removes completions in correct dependency order
- Server errors logged server-side; generic message returned to client

### Security
- Content Security Policy tightened; plain-text uploads removed
- Metadata field validated as JSON before persistence
- Receipt filenames validated against UUID regex before serving (path traversal prevention)
- Receipt ownership verified before serving file (IDOR prevention)

### Performance
- `React.memo` on TaskList; `useMemo` for derived completion state
- `currentMonth` hoisted out of render cycle

### Accessibility
- Progress bar uses `role="progressbar"` with `aria-valuenow/min/max`
- Modal uses `role="dialog"` with `aria-modal` and `aria-labelledby`
- Decorative emoji wrapped in `aria-hidden="true"`
- MonthPicker closes on Escape key

### Mobile
- Touch targets meet 44×44 px minimum
- iOS scroll lock on modal open
- Login form UX improvements on small screens

### Docker
- Switched to `npm ci` for reproducible frontend builds
- Added `.dockerignore` to reduce build context size
