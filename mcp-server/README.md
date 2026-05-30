# Montly MCP Server

An [MCP](https://modelcontextprotocol.io) server that exposes your self-hosted [Montly](../README.md) instance as tools for AI assistants.

Built with the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) — same language as the Montly backend.

## Configuration

| Variable            | Required | Default                  | Description                                  |
|---------------------|----------|--------------------------|----------------------------------------------|
| `MONTLY_URL`        | No       | `http://localhost:8080`  | Base URL of your Montly instance             |
| `MONTLY_TOKEN`      | **Yes**  | —                        | API token (`mt_…`) from Settings             |
| `MCP_PORT`          | No       | — (stdio)                | Set to serve MCP over HTTP instead of stdio  |

Create an API token in Montly → Settings → API Tokens.

## Deployment (Docker — separate container)

The MCP server runs as its own service alongside Montly. Uncomment the `mcp` service in `docker-compose.yml`:

```yaml
services:
  montly:
    build: .
    ports:
      - "127.0.0.1:8080:8080"

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
```

> **Security:** The MCP HTTP transport has no authentication beyond the baked-in API token. Always bind to `127.0.0.1` (localhost) or place it behind an authenticated reverse proxy. Do **not** expose the MCP port to the public internet.

MCP clients connect via Streamable HTTP at `http://your-host:8081/mcp`.

## Usage with Claude Desktop (local, stdio)

When running Montly locally (not in Docker), you can use stdio transport instead. Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "montly": {
      "command": "/path/to/montly/mcp-server/montly-mcp",
      "env": {
        "MONTLY_URL": "http://localhost:8080",
        "MONTLY_TOKEN": "mt_your_token_here"
      }
    }
  }
}
```

When `MCP_PORT` is not set, the server uses stdio transport (default).

## Available tools

### `list_tasks`

List recurring tasks for a given month with completion status.

**Input:**
- `month` (optional) — `YYYY-MM` format. Defaults to current month.

**Output:** JSON with summary counts and per-task details:
- `id`, `title`, `type`, `amount`, `status` (pending/completed/skipped)
- `paid` (actual amount if different), `note`, `has_receipt`, `shared_by`

## Development

```bash
cd mcp-server
go build -o montly-mcp .

# stdio mode (local)
MONTLY_TOKEN=mt_xxx ./montly-mcp

# HTTP mode (Docker-like)
MONTLY_TOKEN=mt_xxx MCP_PORT=8081 ./montly-mcp
```
