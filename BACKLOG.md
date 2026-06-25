# Backlog

Last updated: 2026-06-25 (deep audit — 7 specialist agents + web research)

---

## P1 — Fix immediately

- [ ] **Goroutine storm in FireMonthDigest** — no concurrency cap; one goroutine per user×hook fires on the 1st of every month. Add a semaphore/worker pool. (`backend/webhooks.go:FireMonthDigest`)
- [ ] **Missing SQLite pragmas** — no `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA synchronous=NORMAL`. Foreign keys are unenforced at DB level. (`backend/db.go`)
- [ ] **No UNIQUE constraint on completions(task_id, month)** — concurrent toggle requests can insert duplicate rows. Add DB constraint + idempotent upsert.
- [ ] **CSRF protection missing** — session cookies are not `SameSite=Strict`; no CSRF tokens on mutating endpoints. Set `SameSite=Strict` on session cookie.
- [ ] **SSRF DNS rebinding gap** — `safeWebhookClient` resolves then connects in two steps; a rebinding attack can bypass the IP check. Pin the resolved IP at dial time. (`backend/webhooks.go:safeWebhookClient`)

## P2 — Next sprint

- [ ] **Webhook retry logic** — transient 5xx drops events silently. Add 3-attempt exponential backoff with jitter in `FireWebhooks` and `FireMonthDigest`.
- [ ] **Context propagation to DB** — no `context.Context` passed to any DB query; cancelled requests keep DB work alive. Use `QueryContext`/`ExecContext` throughout `db.go`.
- [ ] **Missing index on completions(task_id, month)** — full table scan on every page load. Add composite index. (`backend/db.go`)
- [ ] **Optimistic toggle debounce** — rapid double-clicks apply the update twice before server responds. Disable control during in-flight request. (`frontend/src/App.jsx`)
- [ ] **Rate limiting gaps** — `/api/auth/setup`, `PATCH /api/auth/password`, and admin user creation have no rate limiting. Extend existing rate-limiter to cover them. (`backend/auth.go`)
- [ ] **Content-Security-Policy header** — `securityHeaders` middleware is missing a CSP. Add restrictive policy. (`backend/main.go`)
- [ ] **Receipt path traversal audit** — confirm download handler strips path components before constructing file path from stored filename. (`backend/handlers.go`)
- [ ] **CSV import month format not validated** — malformed months (e.g. `2026-1`) inserted directly, causing lookup mismatches. Validate and normalise to `YYYY-MM`. (`backend/handlers.go`)
- [ ] **Audit log goroutine leak** — `go h.db.InsertAuditLog(...)` in every handler; if DB is locked, goroutines accumulate. Use a bounded fire-and-forget queue. (`backend/handlers.go`)
- [ ] **User delete doesn't cascade** — deleting a user may leave orphaned tasks/completions/webhooks. Verify or add `ON DELETE CASCADE`. (`backend/db.go`)
- [ ] **N+1 in FireMonthDigest** — 2 sequential queries per user in a loop (`GetWebhooksForUser` + `GetTasks`). Batch with a single query joining users × hooks. (`backend/webhooks.go`)

## P3 — Polish / low priority

- [ ] **Audit log pagination** — confirm `ListAuditLogs` uses SQL `LIMIT/OFFSET`, not in-memory slicing.
- [ ] **`formatAmount` reads locale once on module load** — locale changes mid-session don't reflect until reload. Read from settings context at call time. (`frontend/src/utils.js`)
- [ ] **ReportView skeleton loader** — blank chart area while loading. Add skeleton/loading state. (`frontend/src/ReportView.jsx`)
- [ ] **Webhook test only fires first subscribed event** — expand to allow testing any subscribed event or cycle through them. (`backend/webhooks.go:TestWebhook`)
- [ ] **Amount backfill silent on parse error** — if `metadata.amount` is non-numeric, backfill silently skips. Return error or log clearly. (`backend/db.go:UpdateTaskWithAmountBackfill`)
- [ ] **`writeServerError` may leak schema details** — confirm raw DB errors go to server log only, not response body. (`backend/handlers.go`)
- [ ] **MCP HTTP client has no SSRF protection** — uses `http.DefaultClient`; should use a safe client if `MONTLY_URL` is ever user-supplied. (`mcp-server/client.go`)
- [ ] **MCP `create_task` missing `interval` field** — main API supports it but MCP tool doesn't expose it. (`mcp-server/tools.go`)
- [ ] **MCP tool descriptions are terse** — richer descriptions + parameter examples improve AI assistant usability. (`mcp-server/tools.go`)
- [ ] **No ETag/Last-Modified on GET /api/tasks** — add caching headers to halve round-trips for MCP and other clients.
- [ ] **No E2E coverage of MCP server** — add integration test that runs the full Docker stack and calls MCP tools end-to-end.
- [ ] **FireWebhooks body shared across goroutines** — currently safe but fragile; create a fresh `bytes.NewReader` per goroutine. (`backend/webhooks.go:FireWebhooks`)

---

## Feature ideas (unscheduled)

### High value / low effort
- [ ] **Yearly cost projection** — "this setup costs ~$X/year" on the report page. Simple sum × 12.
- [ ] **Bulk mark-as-done** — "pay all bills" button to toggle all unpaid tasks for the current month.
- [ ] **Completion streaks** — "paid on time X months in a row" per-task badge. Low effort, high motivation signal.

### High value / medium effort
- [ ] **CalDAV / iCal feed** — read-only calendar export of due dates; works with Nextcloud Calendar, Apple Calendar, Google Calendar.
- [ ] **Gotify / Ntfy native push** — push notifications to self-hosted notification servers on digest day (complement to webhooks).
- [ ] **Task reordering** — drag-and-drop custom display order instead of creation order.

### Medium value / medium effort
- [ ] **Monthly spend trend annotations** — mark price-change events on the history chart (e.g. "Netflix +$3 in March").
- [ ] **Quick-add from report view** — "+ Add task" shortcut on the report chart.
- [ ] **Smart recurring detection** — suggest converting one-off tasks to recurring when same amount appears 3+ months in a row.
- [ ] **Apprise notification adapter** — single webhook to local Apprise instance covers 80+ notification services (Telegram, Discord, Matrix, email).

### Lower priority
- [ ] **Import from bank CSV** — accept SEPA/Revolut/Wise CSV formats to pre-fill completion amounts from actual transactions.
