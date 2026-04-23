# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

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
