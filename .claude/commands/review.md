Run a full review of the montly codebase by launching all five specialist agents in parallel, then aggregate their findings into a single prioritised report.

Launch these five agents **simultaneously** (single message, multiple Agent tool calls):

1. **security** — audit file upload handling, path traversal, SQL injection, headers, XSS vectors
2. **go-reviewer** — idiomatic Go, error handling, Chi patterns, SQLite correctness, resource leaks
3. **accessibility** — WCAG 2.1 AA, ARIA, keyboard navigation, focus management
4. **performance** — SQLite indexes, React re-renders, bundle size, Docker image efficiency
5. **ui-ux** — layout, interaction clarity, feedback states, visual hierarchy, mobile readiness

After all five return, produce a single report structured as:

## Review Report

### Critical (fix before shipping)
### High
### Medium
### Low / Nitpicks

Each finding: `[Agent] File:line — description`

Deduplicate overlapping findings. Lead with the most actionable items.
