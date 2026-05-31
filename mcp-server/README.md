# Montly MCP Server

An [MCP](https://modelcontextprotocol.io) server that lets AI assistants interact with your self-hosted [Montly](../README.md) instance — list tasks, mark them done, record payments, skip months, create new tasks, and pull spending reports.

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

Uses `toggle_task` — flips between pending and completed. If a task is currently skipped, toggling it marks it as completed.

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

---

## Limitations

The MCP server exposes a focused subset of the Montly API. It **cannot**:

- Delete or archive tasks
- Upload or manage receipts
- Change user settings or preferences
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

### `toggle_task`

| Parameter | Required | Description |
|-----------|----------|-------------|
| `task_id` | Yes | ID of the task to toggle. |
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
| `amount` | No | Default amount (e.g. `1200.00`). |
| `description` | No | Optional details. |
| `interval` | No | `1` (monthly, default), `2`, `3`, `6`, or `12`. |
| `start_date` | No | First active month in `YYYY-MM` format. |
| `end_date` | No | Last active month in `YYYY-MM` format. |

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
