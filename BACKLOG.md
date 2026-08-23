# Backlog

Last updated: 2026-08-23 (all prior P2 items resolved)

> Many items from the earlier deep audit have since shipped (see CHANGELOG 0.16.0–0.24.1).
> This list contains only what is **genuinely open**, verified against the current codebase.

---

## P1 — Fix immediately

_None currently open._ All prior P1 items are resolved:
WAL + `synchronous=NORMAL` + `foreign_keys` pragmas (`db.go`), digest goroutine cap
(`webhooks.go`), `completions` uniqueness via `PRIMARY KEY (task_id, month)`,
`SameSite=Strict` cookies (`auth.go`), and SSRF dial-time IP pinning
(`safeWebhookClient` dials the validated resolved IP, not the hostname).

## P2 — Next sprint

_None currently open._ All prior P2 items are resolved:
- **OIDC startup resilience** — discovery now runs via `lazyOIDCProvider` (`oidc.go`), retrying with exponential backoff in the background instead of `log.Fatalf`ing at boot. `AuthCodeURL` returns `""` until discovery succeeds; `OIDCLogin` turns that into a 503 rather than a broken redirect.
- **Webhook retry logic** — `deliverWebhook` (`webhooks.go`) retries up to 3 attempts with exponential backoff + jitter on network errors and 5xx; 4xx is not retried. Shared by `FireWebhooks` and `FireMonthDigest`.
- **Context propagation to DB** — all 62 DB-touching methods in `db.go` now take `context.Context` and use `QueryContext`/`ExecContext`/`BeginTx`, threaded from `r.Context()` through every handler (including the OIDC resolution chain). Fire-and-forget paths (audit queue, `UpdateTokenLastUsed`, `FireWebhooks`) use `context.WithoutCancel` so they aren't killed when the request returns.
- **Audit log goroutine leak** — replaced with a bounded `AuditQueue` (`audit.go`, capacity 256) fed by a single worker goroutine; a full queue drops (and logs) an entry rather than blocking the handler.
- **N+1 in FireMonthDigest** — `GetMonthDigestWebhooks` fetches all `month.digest`-subscribed webhooks in one query, grouped by user in Go; `GetTasks` is still called per-user (unavoidable given per-user interval/share resolution) but only for users who actually have a digest webhook.
- **Extend rate limiting to remaining auth endpoints** — `CreateUser`, `OIDCLogin`, and `OIDCCallback` now check the shared `RateLimiter`. OIDC failures (not successes) count against it, via `redirectAuthError`, so legitimate repeat SSO logins never accumulate failures.

## P3 — Polish / low priority

- [ ] **No unique constraint on `users.email`** — email-based SSO linking (`GetUserByEmail`) returns the lowest-id match, so two local accounts sharing an email make linking ambiguous. Add a unique index (or a documented dedupe strategy). (`backend/db.go`, `backend/oidc.go`)
- [ ] **ReportView skeleton loader** — a text "Loading report…" state exists; replace with a proper skeleton/placeholder chart for less layout shift. (`frontend/src/components/ReportView.jsx`)
- [ ] **Webhook test only fires first subscribed event** — `TestWebhook` uses `events[0]`; expand to allow testing any subscribed event or cycle through them. (`backend/webhooks.go:TestWebhook`)
- [ ] **Amount backfill silent on parse error** — if `metadata.amount` is non-numeric, backfill silently skips. Return error or log clearly. (`backend/db.go:UpdateTaskWithAmountBackfill`)
- [ ] **MCP HTTP client has no SSRF protection** — uses a plain `http.Client`; should use a safe client if `MONTLY_URL` is ever user-supplied. (`mcp-server/client.go`)
- [ ] **No ETag/Last-Modified on GET /api/tasks** — add caching headers to halve round-trips for MCP and other clients.
- [ ] **No E2E coverage of MCP server** — add an integration test that runs the full Docker stack and calls MCP tools end-to-end. (OIDC now has E2E via `06-oidc.spec.ts` + the mock IdP; MCP still does not.)

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
- [ ] **RP-initiated SSO logout** — on logout, optionally redirect to the OIDC provider's `end_session_endpoint` so the user is signed out of the IdP too, not just Montly. (`backend/oidc.go`, `frontend` logout flow)

### Medium value / medium effort
- [ ] **Monthly spend trend annotations** — mark price-change events on the history chart (e.g. "Netflix +$3 in March").
- [ ] **Quick-add from report view** — "+ Add task" shortcut on the report chart.
- [ ] **Smart recurring detection** — suggest converting one-off tasks to recurring when same amount appears 3+ months in a row.
- [ ] **Apprise notification adapter** — single webhook to local Apprise instance covers 80+ notification services (Telegram, Discord, Matrix, email).

### Lower priority
- [ ] **Import from bank CSV** — accept SEPA/Revolut/Wise CSV formats to pre-fill completion amounts from actual transactions.
