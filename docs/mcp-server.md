# OrgPulse MCP Server

The MCP (Model Context Protocol) server exposes all admin features of the
OrgPulse dashboard as tools that AI clients can call. It communicates over
stdio and authenticates against the dashboard's REST API using API keys.

## Prerequisites

1. A running OrgPulse instance.
2. An API key created via the admin UI or the `create-admin-key` command.
3. The `mcp-server` binary (built with `go build ./cmd/mcp-server`).

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `DEVLAKE_DASHBOARD_URL` | Yes | Base URL of the dashboard (e.g. `http://localhost:9090`) |
| `DEVLAKE_DASHBOARD_API_KEY` | Yes | API key for authentication |

## Building

```bash
go build -o mcp-server ./cmd/mcp-server
```

## Available Tools

The server exposes 18 tools covering all admin entities:

| Tool | Description |
|---|---|
| `orgs` | Manage organizations (list, get, create, update, delete) |
| `products` | Manage products |
| `teams` | Manage teams |
| `dashboards` | Manage dashboards |
| `sources` | Manage team data sources |
| `subscriptions` | Manage report subscriptions |
| `alerts` | Manage alert definitions |
| `canned_queries` | Manage reusable SQL query templates |
| `canned_scripts` | Manage reusable JavaScript fragments |
| `stored_procedures` | Manage stored procedures (CRUD + deploy + execute SQL) |
| `workflows` | Manage workflows (CRUD + draft/publish/versions/test/archive/lock) |
| `json_schemas` | Manage JSON Schema catalog for workflow test assertions |
| `api_keys` | Manage API keys (requires admin key) |
| `org_product_links` | Manage org-to-product relationships |
| `product_team_links` | Manage product-to-team relationships |
| `team_product_labels` | Manage team product display labels |
| `entity_config` | Manage key-value config for orgs/products/teams |
| `product_overrides` | Manage product-level repository overrides |

Each tool accepts an `operation` parameter that selects the action.

## Client Configuration

### Claude Code

Add the server to your Claude Code MCP settings file
(`~/.claude/settings.json` or project-level `.claude/settings.json`):

```json
{
  "mcpServers": {
    "orgpulse": {
      "command": "/path/to/mcp-server",
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

If you build the binary into your `$PATH`, you can use just the name:

```json
{
  "mcpServers": {
    "orgpulse": {
      "command": "mcp-server",
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

### Claude Desktop

Edit the Claude Desktop config file:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "orgpulse": {
      "command": "/path/to/mcp-server",
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

### OpenCode

Add the server to your `.opencode.json` project configuration:

```json
{
  "mcp": {
    "orgpulse": {
      "type": "stdio",
      "command": "/path/to/mcp-server",
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

### Cursor

Add the server in Cursor's MCP settings (Settings > MCP Servers > Add):

```json
{
  "mcpServers": {
    "orgpulse": {
      "command": "/path/to/mcp-server",
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

### Running with Go directly

Any of the above configurations can use `go run` instead of a pre-built
binary. Replace the `command` with an array:

```json
{
  "mcpServers": {
    "orgpulse": {
      "command": "go",
      "args": ["run", "./cmd/mcp-server"],
      "env": {
        "DEVLAKE_DASHBOARD_URL": "http://localhost:9090",
        "DEVLAKE_DASHBOARD_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

## Usage Examples

Once configured, the AI client can call tools like:

**List all teams:**
```json
{"name": "teams", "arguments": {"operation": "list"}}
```

**Create an org:**
```json
{"name": "orgs", "arguments": {"operation": "create", "data": {"id": "my-org", "name": "My Organization"}}}
```

**Publish a workflow with a note:**
```json
{"name": "workflows", "arguments": {"operation": "publish", "id": 1, "publish_note": "Added error handling"}}
```

**Archive a workflow:**
```json
{"name": "workflows", "arguments": {"operation": "archive", "id": 1}}
```

**Lock a workflow for editing:**
```json
{"name": "workflows", "arguments": {"operation": "lock", "id": 1}}
```

**Create a JSON schema:**
```json
{"name": "json_schemas", "arguments": {"operation": "create", "data": {"name": "my-output-schema", "description": "Validates output structure", "json_schema": "{\"type\":\"object\",\"required\":[\"team\"]}"}}}
```

**Get entity config for a team:**
```json
{"name": "entity_config", "arguments": {"operation": "get", "entity_type": "team", "entity_id": "my-team"}}
```

## Security

- The API key is passed via environment variables, never on the command line.
- The MCP server communicates with the dashboard over HTTP. For production
  deployments, use HTTPS by setting `DEVLAKE_DASHBOARD_URL` to an `https://` URL.
- Admin-only operations (API key management) require an admin API key.
- The server logs to stderr; stdout is reserved for the MCP protocol.
