# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

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
