# Montly MCP Server

An [MCP](https://modelcontextprotocol.io) server that lets AI assistants interact with your self-hosted [Montly](../README.md) instance — list tasks, mark them done, record payments, skip months, create, edit, archive and delete tasks, pull spending reports, export completion history, and read or change your display settings.

Built with the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Quickstart

### 1. Create an API token

Open Montly → **Settings → API Tokens → Create token**. Copy the `mt_…` value — you'll need it in step 3.

### 2. Choose your setup

**Docker (recommended)** — add the MCP service to your `docker-compose.yml`:

```yaml
  mcp:
    image: ghcr.io/lucaslra/montly-mcp:latest
    ports:
      - "127.0.0.1:8081:8081"
    environment:
      MONTLY_URL: "http://montly:8080"
      MONTLY_TOKEN: "mt_your_token_here"
      MCP_PORT: "8081"
    depends_on:
      - montly
    restart: unless-stopped
```

Then `docker compose up -d mcp`.

**Local binary** — build and run directly (requires Go 1.25+):

```bash
cd mcp-server
go build -o montly-mcp .
MONTLY_TOKEN=mt_your_token_here ./montly-mcp
```

When `MCP_PORT` is not set, the server uses stdio transport (launched as a subprocess by your MCP client).

### 3. Configure your MCP client

Pick your client below and paste the config.

#### Claude Desktop (local, stdio)

`~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "montly": {
      "command": "/path/to/mcp-server/montly-mcp",
      "env": {
        "MONTLY_URL": "http://localhost:8080",
        "MONTLY_TOKEN": "mt_your_token_here"
      }
    }
  }
}
```

#### Claude Desktop (remote, HTTP)

```json
{
  "mcpServers": {
    "montly": {
      "url": "http://localhost:8081/mcp"
    }
  }
}
```

#### Claude Code

```bash
claude mcp add montly -- /path/to/mcp-server/montly-mcp
```

Set the env vars in your shell or in `.claude/settings.json`:

```json
{
  "env": {
    "MONTLY_URL": "http://localhost:8080",
    "MONTLY_TOKEN": "mt_your_token_here"
  }
}
```

#### Cursor

In Cursor settings → MCP Servers, add:

```json
{
  "montly": {
    "command": "/path/to/mcp-server/montly-mcp",
    "env": {
      "MONTLY_URL": "http://localhost:8080",
      "MONTLY_TOKEN": "mt_your_token_here"
    }
  }
}
```

---

## What you can do

Once connected, just ask your AI assistant naturally. Here are some examples:

### Check your tasks

> "What's due this month?"
>
> "Show me my tasks for January 2026"
>
> "How many tasks are still pending?"

Uses `list_tasks` — returns each task with its status (pending/completed/skipped), amount, type, and whether a receipt is attached.

### Mark tasks as done

> "Mark rent as paid"
>
> "I paid the electricity bill"
>
> "Undo — unmark Netflix"

Uses `complete_task` to mark a task done (a skipped month becomes completed) and `uncomplete_task` to undo it. Both check the current state first, so repeating a request never flips it back. Undoing discards that month's recorded amount, note and receipt, so MCP clients that honour tool annotations may ask for confirmation before `uncomplete_task`.

### Record what you actually paid

> "I paid $1,250 for rent this month"
>
> "Set the Netflix amount to 17.99"
>
> "Add a note to rent: paid via wire transfer"

Uses `update_completion` — sets the actual paid amount or attaches a note. The task must already be marked as done.

### Skip a month

> "Skip gym this month"
>
> "Un-skip the car insurance"

Uses `skip_task` — marks a task as intentionally skipped. Skipped tasks are excluded from progress bars and spending totals.

### Create new tasks

> "Add a new monthly task for car insurance, $280"
>
> "Create a quarterly reminder for dentist appointment"
>
> "Add a subscription for Spotify at $11.99"

Uses `create_task` — creates a recurring task with the given title, type, amount, and interval.

### Get spending reports

> "How's my spending trending?"
>
> "Show me a report for the last 6 months"
>
> "What's my monthly average spending?"

Uses `get_report` — returns 6 months of history and 3 months of forecast with per-month totals for tasks completed, amounts due, and amounts paid.

### Edit tasks

> "Rent went up to 1300"
>
> "Make the car insurance quarterly"
>
> "The gym membership ends in December"

Uses `get_task` to read a task's full details and `update_task` to change it. Only the fields you mention are changed. A new amount applies straight away (there is no future effective date). If the task already had an amount, past completed months that had no amount of their own keep the old one, so history stays accurate; if it had none, those months pick up the new amount. Editing, archiving and deleting only work on tasks you own; on a task shared with you, the tool says so instead of failing with "not found", and points to what you can still do (mark done, skip, record an amount or note).

### Archive or delete tasks

> "I cancelled Spotify, archive it"
>
> "What have I archived?"
>
> "Bring back the gym task"

Uses `archive_task` (and `list_archived_tasks` to find archived tasks again). Archiving hides a task from monthly lists but keeps its history. `delete_task` removes a task and all its history for good. MCP clients that honour tool annotations may ask for confirmation before running it.

### Look back over a longer period

> "What did I pay for electricity in 2025?"
>
> "Export everything I completed between March and June"

Uses `export_completions` — returns every completed or skipped entry in a month range (defaults to the current year so far). Large exports are capped at 500 rows, keeping the most recent whole months.

### Check or change your settings

> "What currency am I using?"
>
> "Switch amounts to European number format"
>
> "Start my fiscal year in April"

Uses `get_settings` and `update_settings`. Assistants can also read `currency` and `number_format` to present amounts the way you see them in the app.

---

## Limitations

The MCP server exposes a focused subset of the Montly API. It **cannot**:

- Share tasks or manage collaborators
- Upload or manage receipts
- Import completions from CSV
- Change your password
- Manage users, tokens, or webhooks
- Access other users' data (scoped to the token's owner)

These actions are available only through the Montly web UI.

---

## Tool reference

### `list_tasks`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `month` | No | `YYYY-MM` format. Defaults to current month. |

Returns JSON with summary counts (`total`, `completed`, `skipped`, `pending`) and per-task details (`id`, `title`, `type`, `amount`, `status`, `paid`, `note`, `has_receipt`, `shared_by`).

### `get_report`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `month` | No | Anchor month in `YYYY-MM` format. Defaults to current month. |

Returns per-month summaries covering 6 months before and 3 months after the anchor: `task_count`, `completed`, `skipped`, `total_due`, `total_paid`.

### `complete_task`

Marks the task done for the month; a skipped month becomes completed. Does nothing if it is already completed.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |
| `month` | No | `YYYY-MM` format. Defaults to current month. |

### `uncomplete_task`

Sets a completed task back to pending, discarding the month's amount, note and receipt (the reply lists what was discarded). Does nothing if the task is pending or skipped. Annotated as destructive.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |
| `month` | No | `YYYY-MM` format. Defaults to current month. |

### `skip_task`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task to skip or un-skip. |
| `month` | No | `YYYY-MM` format. Defaults to current month. |

### `update_completion`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |
| `month` | No | `YYYY-MM` format. Defaults to current month. |
| `amount` | No | Actual amount paid. Empty string clears the override. |
| `note` | No | Note to attach. At least one of `amount` or `note` is required. |

### `create_task`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `title` | Yes | Name of the recurring task. |
| `type` | No | `payment`, `subscription`, `bill`, `reminder`, or empty. |
| `amount` | No | Default amount as a plain number (e.g. `1200.00`, no currency symbol). |
| `description` | No | Optional details. |
| `interval` | No | `1` (monthly, default), `2`, `3`, `6`, or `12`. |
| `start_date` | No | First active month in `YYYY-MM` format. |
| `end_date` | No | Last active month in `YYYY-MM` format. |

### `get_task`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |

Returns `id`, `title`, `description`, `type`, `amount`, `interval`, `start_date`, `end_date`, `archived_at`, and `owned_by_you`. `shared_with` is included only for tasks you own. Works for owned and shared tasks.

### `update_task`

Owner only: works on tasks where `owned_by_you` is true (or that have no `shared_by` in `list_tasks`). Reads the task, then writes it back with only the given fields changed. Legacy `metadata` is reset to `{}`, as the web UI does.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |
| `title` | No | New title (cannot be empty). |
| `type` | No | `payment`, `subscription`, `bill`, `reminder`, or empty string to clear. |
| `amount` | No | New default amount as a plain number, or empty string to clear. If the task already had an amount, past completions without their own amount keep the old one. |
| `description` | No | New description, or empty string to clear. |
| `interval` | No | `1`, `2`, `3`, `6`, or `12`. |
| `start_date` | No | `YYYY-MM`, or empty string to clear. |
| `end_date` | No | `YYYY-MM`, or empty string to clear. |

At least one field besides `task_id` is required.

### `archive_task`

Owner only.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |
| `unarchive` | No | `true` to restore an archived task. Defaults to `false` (archive). |

### `list_archived_tasks`

No parameters. Returns `id`, `title`, `type`, `amount`, `interval`, `archived_at` for each archived task.

### `delete_task`

Owner only. **Permanent:** removes the task, all its completions, notes and receipt files. Annotated as destructive.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task. |

### `export_completions`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `from` | No | First month, `YYYY-MM`. Defaults to January of the year of `to`. |
| `to` | No | Last month, `YYYY-MM`. Defaults to the current month. |

Returns the resolved `from`/`to`, `total`, `rows` (`title`, `type`, `month`, `status`, `amount`, `has_receipt`), and, when more than 500 rows matched, `truncated: true` plus `earliest_month`: at most 500 rows are returned, cut at a month boundary so the most recent whole months are kept, and the months before `earliest_month` need a separate query. Only completed and skipped entries for tasks you own are included, archived tasks among them. For completed rows an empty `amount` means the task's default amount applied; skipped rows were not paid and carry no amount. Rows carry no task ID.

### `get_settings`

No parameters. Returns `currency`, `number_format`, `date_format`, `color_mode`, `task_sort`, `completed_last`, and `fiscal_year_start` (all strings, as stored, even though `update_settings` takes `completed_last` as a boolean and `fiscal_year_start` as a number).

### `update_settings`

Only the fields you pass are changed; at least one is required. Returns the full settings afterwards. Invalid values are rejected by the backend with a message listing the allowed ones.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `currency` | No | Currency symbol, at most 10 bytes (`€` counts as 3). An empty string resets it to `€`. |
| `number_format` | No | `en` (1,234.56) or `eu` (1.234,56). |
| `date_format` | No | `long` (April 2026), `short` (Apr 2026), `numeric` (04/2026), or `iso` (2026-04). |
| `color_mode` | No | `system`, `light`, or `dark`. |
| `task_sort` | No | `type` (grouped by type; the out-of-the-box setting), `name` (alphabetical), or `default` (creation order, despite the name). |
| `completed_last` | No | Boolean: move completed tasks to the bottom. |
| `fiscal_year_start` | No | `1`–`12`. Only affects the web app's Reports view, not `get_report`. |

---

## Configuration reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MONTLY_URL` | No | `http://localhost:8080` | Base URL of your Montly instance |
| `MONTLY_TOKEN` | **Yes** | — | API token (`mt_…`) from Settings |
| `MCP_PORT` | No | — (stdio) | Set to serve MCP over HTTP instead of stdio |

> **Security:** The MCP HTTP transport has no authentication beyond the baked-in API token. Always bind to `127.0.0.1` (localhost) or place it behind an authenticated reverse proxy. Do **not** expose the MCP port to the public internet.

---

## Development

```bash
cd mcp-server
go build -o montly-mcp .
go test ./...

# stdio mode (local)
MONTLY_TOKEN=mt_xxx ./montly-mcp

# HTTP mode (Docker-like)
MONTLY_TOKEN=mt_xxx MCP_PORT=8081 ./montly-mcp
```
