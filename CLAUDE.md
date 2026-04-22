# Montly

Self-hosted monthly recurring task tracker. Go+Chi+SQLite backend, React+Vite frontend, deployed via Docker.

## Stack
- **Backend:** Go 1.22, Chi router, modernc.org/sqlite (pure Go, no CGO)
- **Frontend:** React 19 + Vite, plain CSS, hooks pattern
- **Infra:** Multi-stage Dockerfile, docker-compose, Makefile

## Dev workflow
- `make up` — build image and start (standard)
- `make dev-backend` + `make dev-frontend` — local dev (two terminals; Vite proxies /api to :8080)
- `make setup` — first-time: go mod tidy + npm install

## Key conventions
- All SQLite queries and migrations live in `backend/db.go`; migrations use idempotent ALTER TABLE
- All HTTP handlers in `backend/handlers.go`; keep them thin — data logic belongs in db.go
- Frontend API calls are centralized in `frontend/src/api.js`
- Receipt files are uuid-named and stored in DATA_DIR/receipts/; unchecking a task removes the receipt AND the file
- A mobile app will be added in the future — keep API design flexible

## Available agents
Use these for focused reviews (invoke via subagent):
- **ui-ux** — layout, interaction design, feedback states, mobile readiness
- **security** — file upload safety, path traversal, SQL injection, HTTP headers
- **go-reviewer** — idiomatic Go, Chi patterns, SQLite correctness, error handling
- **accessibility** — WCAG 2.1 AA, ARIA, keyboard navigation, focus management
- **performance** — SQLite indexes, React re-renders, bundle size, Docker image
- **mobile** — touch targets, viewport behaviour, iOS/Android quirks, PWA readiness

## Slash commands
- `/review` — runs all five review agents in parallel and produces a single prioritised report
