# Contributing

Thanks for your interest in contributing to Montly.

## Before you start

For anything beyond a small bug fix, open an issue first to discuss the change. This saves effort if the direction doesn't fit the project's goals.

## Setup

```bash
git clone <repo>
cd montly
make setup          # go mod tidy + npm install (backend, frontend, and e2e)
make dev-backend    # terminal 1 — Go API on :8080
make dev-frontend   # terminal 2 — Vite dev server on :5173
```

Open `http://localhost:5173`. On first run you'll be prompted to create an admin account.

## Tests

```bash
make test           # runs both Go and frontend tests
cd backend && go test ./...
cd frontend && npm test
```

### E2E tests

Playwright tests exercise the full stack (app + DB) inside Docker:

```bash
make e2e            # headless — runs entirely in Docker, cleans up automatically
make e2e-headed     # visible browser — app in Docker, Playwright runs locally
```

For headed mode, install the browser binary once:

```bash
cd e2e && npx playwright install chromium
```

All tests must pass before a PR can be merged.

## Code conventions

- **Backend:** handlers in `handlers.go` / `auth.go` / `webhooks.go`; all DB queries and migrations in `db.go`. Keep handlers thin — logic in helpers.
- **Frontend:** API calls centralised in `api.js`; shared utilities in `utils.js`. No new dependencies without discussion.
- **SQL:** idempotent `ALTER TABLE` migrations; parameterised queries only; no raw string interpolation.
- **Commits:** use conventional-ish prefixes (`feat:`, `fix:`, `test:`, `chore:`, `docs:`).

## Submitting a PR

1. Fork → feature branch → PR against `main`.
2. Describe *what* changed and *why*.
3. If it touches the UI, include a screenshot or brief description of what you tested.
4. Keep PRs focused — one logical change per PR.

## License

By contributing you agree your work is licensed under the [MIT License](LICENSE).
