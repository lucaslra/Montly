# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

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
