# REST API Reference

The REST API provides full CRUD access to the dashboard configuration database. It is designed for automation from tools like Ansible, CI pipelines, or custom scripts.

All endpoints are under `/api/v1/` and require authentication — either an API key or a proxy-forwarded identity (see below). The assembled JSON endpoints (`/api/orgs`, `/api/products`, `/api/teams`) also require authentication and return only the caller's Rover grant.

## Authentication

Authentication for `/api/v1/*` and the assembled JSON endpoints depends on the access method: **interactive browser sessions** vs. **programmatic / machine-to-machine (M2M) automation**. Tenant visibility is a separate step: SSO and API keys authenticate the caller, and Rover group membership decides which orgs, products, and teams they can see.

### 1. Browser Sessions (Authenticating Proxy & Local Dev)

- **Proxy authentication**: A trusted authenticating proxy forwards `X-Forwarded-User` and `X-Forwarded-Email` headers to the application. Users listed in `ADMIN_USERS` (or when `ADMIN_USERS=*`) are granted **Admin** privileges. The application trusts these headers, so the proxy must replace any client-supplied values and direct access to the application port must be blocked. Browser users do not need API keys.
- **Local Development**: When running locally with `make dev` or setting `AUTH_TEST_MODE=true` / `DEV_ADMIN_OVERRIDE=true`, requests are automatically granted access without needing proxy sidecars or API keys.

### 2. Programmatic & Machine-to-Machine (M2M) Access

API keys are **optional** and reserved for headless/scripted automation (e.g., CI/CD pipelines, curl, Ansible, FastMCP servers).

Requests carrying an API key authenticate via one of:

- **Authorization header**: `Authorization: Bearer <key>`
- **X-API-Key header**: `X-API-Key: <key>`

API keys are managed via the `/api/v1/keys` endpoints (admin only). To bootstrap an initial admin key for programmatic access, see [Bootstrapping](#bootstrapping) below.

### Roles & RBAC

| Role | Authentication Mechanism | Permissions & Scope |
|------|--------------------------|---------------------|
| **Admin** | `X-Forwarded-User` matched in `ADMIN_USERS` list (or `*`), `DEV_ADMIN_OVERRIDE=true`, or Admin API Key (`is_admin: true`) | Full unrestricted read/write access to all global orgs, products, teams, dashboards, sources, config, workflows, queries, scripts, API keys, and all users' alerts & subscriptions. |
| **Operator** | Rover group listed in `OPERATOR_ROVER_GROUPS` | Sees every configured org, product, and team, and can update those records including Rover groups. Not a platform admin: workflow editing, the SQL console, API keys, and deletes stay admin-only. |
| **Contributor / Viewer** | `X-Forwarded-User` present (not in `ADMIN_USERS`) or a non-admin API key with `scoped_user` set | Reads and updates only the orgs, products, and teams whose `auth_group` matches a Rover group from `ROVER_GROUPS_CSV`. A team group sees that team; a product group sees that product and its teams; an org group sees that org. Parent names stay visible so a team member can navigate. Objects outside the grant are `404`. A non-admin API key with no `scoped_user` sees no tenant data. Self-service read/write access to own subscriptions and alerts stays scoped to the forwarded email. |
| **Anonymous** | None (direct unauthenticated access) | Read-only access to `/health/live`, `/health/ready`, and `/metrics`, plus embedded frontend static assets. Assembled config, `/api/v1/*`, and live workflow execution are rejected with `401 Unauthorized`. |

### Current User Identity

```
GET /api/v1/auth/me
```

Returns the authenticated caller's identity:

```json
{
  "authenticated": true,
  "username": "jdoe",
  "email": "jdoe@example.com",
  "is_admin": true,
  "auth_type": "forwarded_identity",
  "roles": ["admin", "contributor"],
  "grant": {
    "operator": false,
    "orgs": [{ "id": "example-org", "aggregate": true }],
    "products": [{ "id": "product-1", "aggregate": true }],
    "teams": ["team-1"]
  }
}
```

## Error Responses

All errors return JSON with an `error` field:

```json
{"error": "description of the problem"}
```

| Status | Meaning |
|--------|---------|
| `400` | Bad request (invalid JSON, missing required fields) |
| `401` | Missing or invalid API key |
| `403` | Admin access required |
| `404` | Resource not found |
| `409` | Conflict (duplicate ID) |
| `500` | Internal server error |

## Data Model

The database stores a hierarchy: **orgs** contain **products**, which contain **teams**. Teams have **data sources** (git repos and work-item trackers). **Dashboards** can be attached at any level. **Entity config** stores key-value settings for orgs, products, and teams. **Subscriptions** define periodic email reports scoped to a product and/or team. **Alerts** define threshold-based notifications that evaluate a JS rule against a metrics endpoint. **Stored procedures** define reusable SQL logic that can be deployed to the data database and called from workflow queries. **Workflows** are named data pipelines — see [workflows.md](workflows.md) for full documentation.

```
org
├── products (linked via org_products)
├── config (entity_config key-value pairs)
└── dashboards

product
├── teams (linked via product_teams)
├── config (entity_config key-value pairs)
├── overrides (product-level repo remapping)
└── dashboards

team
├── product labels (display names)
├── config (entity_config key-value pairs)
├── sources
│   ├── repos (with whitelist, excluded dashboard types)
│   └── workitem projects
└── dashboards
    ├── blueprint IDs
    └── diagrams

subscription (standalone, scoped to product/team)
├── elements (URL + payload)
└── scheduling metadata (interval, last_run, ai_analytics_url)

alert (standalone, scoped to product/team)
└── JS rule evaluated against metrics endpoint response
```

---

## Orgs

### List orgs

```
GET /api/v1/orgs
```

Response `200`:
```json
[
  {"id": "myorg", "name": "My Organization"}
]
```

### Create org

```
POST /api/v1/orgs
```

```json
{"id": "myorg", "name": "My Organization"}
```

Response `201`: returns the created org. `409` if the ID already exists.

### Get org

```
GET /api/v1/orgs/{id}
```

Response `200`:
```json
{"id": "myorg", "name": "My Organization"}
```

### Update org

```
PUT /api/v1/orgs/{id}
```

```json
{"name": "New Name"}
```

Response `200`: returns the updated org. `404` if not found.

### Delete org

```
DELETE /api/v1/orgs/{id}
```

Response `204`. Cascades to org_products and org dashboards. `404` if not found.

---

## Products

### List products

```
GET /api/v1/products
```

Response `200`:
```json
[
  {"id": "prod-a", "name": "Product A", "contact_email": "owner@example.com"}
]
```

### Create product

```
POST /api/v1/products
```

```json
{
  "id": "prod-a",
  "name": "Product A",
  "contact_email": "owner@example.com"
}
```

`contact_email` is optional. Response `201`.

### Get product

```
GET /api/v1/products/{id}
```

### Update product

```
PUT /api/v1/products/{id}
```

```json
{"name": "Updated Name", "contact_email": "new@example.com"}
```

### Delete product

```
DELETE /api/v1/products/{id}
```

Cascades to product_teams, product dashboards, sources, and overrides.

---

## Teams

### List teams

```
GET /api/v1/teams
```

Response `200`:
```json
[
  {
    "id": "team-1",
    "name": "Team One",
    "contact_email": "team@example.com",
    "fetch_url": "https://devlake/api/blueprints/{{blueprintid}}/trigger",
    "fetch_check_url": "https://devlake/api/blueprints/{{blueprintid}}/pipelines"
  }
]
```

### Create team

```
POST /api/v1/teams
```

```json
{
  "id": "team-1",
  "name": "Team One",
  "contact_email": "team@example.com",
  "fetch_url": "...",
  "fetch_check_url": "..."
}
```

All fields except `id` and `name` are optional.

### Get team

```
GET /api/v1/teams/{id}
```

### Update team

```
PUT /api/v1/teams/{id}
```

### Delete team

```
DELETE /api/v1/teams/{id}
```

Cascades to sources, repos, dashboards, and product labels.

---

## Relationships

### Org → Product links

```
GET /api/v1/orgs/{id}/products
```

Response `200`:
```json
[
  {"id": "prod-a", "sort_order": 0},
  {"id": "prod-b", "sort_order": 1}
]
```

```
PUT /api/v1/orgs/{id}/products
```

Replaces all product links for the org:
```json
[
  {"id": "prod-a", "sort_order": 0},
  {"id": "prod-b", "sort_order": 1}
]
```

### Product → Team links

```
GET /api/v1/products/{id}/teams
```

```
PUT /api/v1/products/{id}/teams
```

Same format as org→product links, with team IDs.

Both `PUT` endpoints need update rights on the parent. Linking a child gives access to it through the parent, so a non-admin may only **add** a product or team they can already update (a `403` names the first one that fails). Removing and reordering existing children need only the parent permission.

### Team product labels

Display names of products a team belongs to (stored separately from the actual relationship for backward compatibility).

```
GET /api/v1/teams/{id}/product-labels
```

Response `200`:
```json
["Product A", "Product B"]
```

```
PUT /api/v1/teams/{id}/product-labels
```

```json
["Product A", "Product B"]
```

---

## Entity Config

Key-value configuration for orgs, products, and teams. The `{type}` path parameter must be `org`, `product`, or `team`.

### Get config

```
GET /api/v1/config/{type}/{id}
```

Response `200`:
```json
{
  "cycleTimeSlowHours": "168",
  "cycleTimeCriticalHours": "336",
  "analyticsprompt": "Focus on deployment frequency."
}
```

Returns an empty object `{}` if no config exists.

Stored integration tokens (`devlake_<plugin>_token`) are returned as `********` when set, so callers can tell a token exists without reading it. `devlake_*` keys are managed by the DevLake onboarding endpoints and are preserved on `PUT`, so sending the placeholder back never changes a token.

### Replace config

```
PUT /api/v1/config/{type}/{id}
```

Replaces all config keys for the entity:
```json
{
  "cycleTimeSlowHours": "168",
  "cycleTimeCriticalHours": "336"
}
```

### Delete config

```
DELETE /api/v1/config/{type}/{id}
```

Removes all config keys for the entity. Response `204`.

The known keys, with labels and descriptions, are listed by `GET /api/v1/config-keys` (source: `schemas/config-keys.json`). Some dashboard types need specific team config keys, which are sent to their workflows; see [Dashboard request parameters](#dashboard-request-parameters).

---

## Dashboards

Dashboards can be attached to orgs, products, or teams. They are identified by an auto-increment `auto_id` for CRUD operations.

### List dashboards

```
GET /api/v1/dashboards?owner_type=team&owner_id=team-1
```

Both `owner_type` and `owner_id` query parameters are required. `owner_type` is one of `org`, `product`, `team`.

Response `200`:
```json
[
  {
    "auto_id": 42,
    "owner_type": "team",
    "owner_id": "team-1",
    "dashboard_id": "pr-cycletime",
    "type": "prcycletime",
    "title": "PR Cycle Time",
    "description": "Tracks PR merge times",
    "chat_url": "https://metrics.example.com/chat",
    "ai_analytics_url": "https://metrics.example.com/analytics",
    "default_source": "gitrepos",
    "connection_id": "1",
    "jira_project": "AAP",
    "blueprint_ids": ["bp-123", "bp-456"],
    "diagrams": [
      {
        "auto_id": 100,
        "position": 0,
        "type": "json",
        "endpoint": "https://metrics.example.com/diagram1",
        "span": 2,
        "title": "Cycle Time Trend",
        "ai_analytics": true,
        "description": "Shows 30-day trend",
        "params": {"view": "trend"}
      }
    ]
  }
]
```

`params` is optional. See [Dashboard request parameters](#dashboard-request-parameters).

### Create dashboard

```
POST /api/v1/dashboards
```

```json
{
  "owner_type": "team",
  "owner_id": "team-1",
  "dashboard_id": "pr-cycletime",
  "type": "prcycletime",
  "title": "PR Cycle Time",
  "description": "Tracks PR merge times",
  "blueprint_ids": ["bp-123"],
  "diagrams": [
    {
      "position": 0,
      "type": "json",
      "endpoint": "https://metrics.example.com/diagram1",
      "span": 2,
      "title": "Cycle Time Trend",
      "ai_analytics": false
    }
  ]
}
```

Required fields: `owner_type`, `owner_id`, `dashboard_id`, `type`, `title`. All other fields are optional. A diagram's `params`, when present, must be a JSON object; anything else returns `400`.

Response `201`: returns the created dashboard with `auto_id`.

### Get dashboard

```
GET /api/v1/dashboards/{auto_id}
```

Returns the dashboard with its blueprint IDs and diagrams inline.

### Update dashboard

```
PUT /api/v1/dashboards/{auto_id}
```

Updates the dashboard and **replaces** all blueprint IDs and diagrams with the ones in the request body.

### Delete dashboard

```
DELETE /api/v1/dashboards/{auto_id}
```

Cascades to blueprint IDs and diagrams. Response `204`.

### Dashboard request parameters

The dashboard frontend POSTs a standardized body to each diagram's `endpoint`: `owner` and `name` (selected repos; not sent for dashboard types that don't use repos, such as `aisdlc`), `projects` (issue dashboards), `from` and `to` (unix seconds), and, when set on the dashboard, `connectionid` and `jiraproject`. Two sources of extra parameters are added to that body. They never override a standard key.

1. **Dashboard-type parameters.** These are hardcoded per dashboard type in the frontend (`DASHBOARD_TYPE_PARAMS` in `web/patternfly/src/charts/diagramRequest.ts`). They are built from team config entries, for the team the dashboard is attached to. All listed config entries are mandatory.

   | Dashboard type | Team config entry | Request parameter |
   |---|---|---|
   | `aisdlc` | `rfe_jira_project` (comma-separated keys) | `rfe_jira_projects: ["<key>", …]` |
   | `aisdlc` | `strat_jira_project` (comma-separated keys) | `strat_jira_projects: ["<key>", …]` |
   | `aisdlc` | `rfe_station_id` (comma-separated ids) | `rfe_station_ids: ["<id>", …]` |
   | `aisdlc` | `strategy_station_id` (comma-separated ids) | `strategy_station_ids: ["<id>", …]` |

   `aisdlc` dashboards also don't use the team's repos: they send no `owner`/`name` and show no repository filter (`omitRepos` in the same definition).

   If an entry has no value, the dashboard shows a warning naming the missing keys. The diagrams still load, without that parameter, and the workflows fall back to their own defaults. Dashboards of other types don't load team config for this.
2. **Diagram parameters.** A diagram's optional `params` object is added to that diagram's request only. For example, `{"view": "criteria"}` selects a view of a multi-view workflow. If a diagram parameter has the same name as a dashboard-type parameter, the diagram parameter wins.

---

## Sources

Sources define the data pipelines for a team: git repositories (`gitrepos`) or work-item trackers (`workitems`).

### List sources

```
GET /api/v1/teams/{teamId}/sources
```

Response `200`:
```json
[
  {
    "id": "gitrepos",
    "type": "gitrepos",
    "config": {"some": "value"},
    "repos": [
      {
        "id": "my-repo",
        "type": "github",
        "owner": "my-org",
        "name": "my-repo",
        "excluded_dashboard_types": ["codecoverage"],
        "whitelist": ["user1", "user2"]
      }
    ]
  },
  {
    "id": "aap-jira",
    "type": "workitems",
    "projects": [
      {
        "id": "aap",
        "project_name": "AAP Project",
        "connection_id": "1",
        "jira_project": "AAP",
        "type": "jira"
      }
    ]
  }
]
```

### Create source

```
POST /api/v1/teams/{teamId}/sources
```

```json
{
  "id": "gitrepos",
  "type": "gitrepos",
  "config": {"custom": "setting"},
  "repos": [
    {
      "id": "my-repo",
      "type": "github",
      "owner": "my-org",
      "name": "my-repo",
      "excluded_dashboard_types": ["codecoverage"],
      "whitelist": ["user1"]
    }
  ]
}
```

Include `repos` for `gitrepos` sources and `projects` for `workitems` sources. They are created together with the source in a single transaction.

### Get source

```
GET /api/v1/teams/{teamId}/sources/{sourceId}
```

Returns the source with repos/projects inline.

### Update source

```
PUT /api/v1/teams/{teamId}/sources/{sourceId}
```

Updates the source and **replaces** all repos/projects with the ones in the request body.

### Delete source

```
DELETE /api/v1/teams/{teamId}/sources/{sourceId}
```

Cascades to repos, excluded dashboard types, whitelists, and workitem projects. Response `204`.

---

## Product Overrides

Product-level repo-to-team mappings that override the team-level source config.

### Get overrides

```
GET /api/v1/products/{id}/overrides
```

Response `200`:
```json
[
  {
    "id": "shared-repo",
    "type": "github",
    "owner": "my-org",
    "name": "shared-repo",
    "teams": ["team-a", "team-b"]
  }
]
```

### Replace overrides

```
PUT /api/v1/products/{id}/overrides
```

Replaces all override repos for the product:
```json
[
  {
    "id": "shared-repo",
    "type": "github",
    "owner": "my-org",
    "name": "shared-repo",
    "teams": ["team-a", "team-b"]
  }
]
```

---

## Subscriptions

Subscriptions define periodic email report configurations. A subscription has an email address, optional product/team scope, an AI analytics URL, an interval, and a set of report elements.

Each element has a URL and an optional JSON payload. When the subscription is due, the scheduler sends each element's payload to its URL, renders charts, sends the response to the subscription's AI analytics URL for analysis, and emails the assembled report. If no AI analytics URL is set, the AI analysis step is skipped.

Admins may use any URL. For other callers, element URLs and `ai_analytics_url` must point at a workflow endpoint of this dashboard (`WORKFLOW_BASE_URL/workflows/...`) or of the workflow execution service (`WORKFLOW_EXEC_URL/workflows/...`), or equal the configured `AI_ANALYTICS_URL`; anything else returns `403`. The same rule applies to alert `url` and `ai_analytics_url`. Report and alert requests never reach link-local or cloud metadata addresses.

### List subscriptions

```
GET /api/v1/subscriptions
```

Response `200`:
```json
[
  {
    "id": 1,
    "email": "team-lead@example.com",
    "product_id": "prod-a",
    "team_id": "team-1",
    "ai_analytics_url": "https://metrics.example.com/analytics",
    "interval_days": 7,
    "last_run_at": "2026-07-15T09:00:00Z",
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-15T09:00:00Z",
    "elements": [
      {
        "id": 10,
        "sort_order": 0,
        "url": "https://metrics.example.com/weekly-report",
        "payload": {"format": "pdf"}
      }
    ]
  }
]
```

### Create subscription

```
POST /api/v1/subscriptions
```

```json
{
  "email": "team-lead@example.com",
  "product_id": "prod-a",
  "team_id": "team-1",
  "ai_analytics_url": "https://metrics.example.com/analytics",
  "interval_days": 7,
  "elements": [
    {
      "sort_order": 0,
      "url": "https://metrics.example.com/weekly-report",
      "payload": {"format": "pdf"}
    }
  ]
}
```

Required fields: `email`. All other fields are optional.

Response `201`: returns the created subscription with `id`.

### Get subscription

```
GET /api/v1/subscriptions/{id}
```

Returns the subscription with its elements inline.

### Update subscription

```
PUT /api/v1/subscriptions/{id}
```

Updates the subscription and **replaces** all elements with the ones in the request body.

### Delete subscription

```
DELETE /api/v1/subscriptions/{id}
```

Cascades to elements. Response `204`. `404` if not found.

---

## Alerts

Alerts define threshold-based email notifications. Each alert monitors a metrics endpoint and evaluates a JavaScript rule against the response. When the rule returns `true`, the metrics response is sent to an AI analytics endpoint and the analysis is emailed. A rule that runs longer than 10 seconds is stopped and treated as an error.

The rule is a JavaScript expression (ECMAScript 5.1) that receives the full metrics endpoint response as the `data` variable. It must evaluate to a boolean. Examples:

- `data.atomicMetricsValue > 168` — scalar comparison
- `data.atomicMetricsValue.cycleTime > 168 && data.atomicMetricsValue.count > 5` — object field access
- `data.status === "critical"` — string comparison

### List alerts

```
GET /api/v1/alerts
```

Response `200`:
```json
[
  {
    "id": 1,
    "email": "oncall@example.com",
    "product_id": "prod-a",
    "team_id": "team-1",
    "url": "https://metrics.example.com/metrics",
    "payload": {},
    "ai_analytics_url": "https://metrics.example.com/analyze",
    "rule": "data.atomicMetricsValue > 168",
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-01T10:00:00Z"
  }
]
```

### Create alert

```
POST /api/v1/alerts
```

```json
{
  "email": "oncall@example.com",
  "product_id": "prod-a",
  "team_id": "team-1",
  "url": "https://metrics.example.com/metrics",
  "payload": {},
  "ai_analytics_url": "https://metrics.example.com/analyze",
  "rule": "data.atomicMetricsValue > 168"
}
```

Required fields: `email`, `url`, `rule`. All other fields are optional.

Response `201`: returns the created alert with `id`.

### Get alert

```
GET /api/v1/alerts/{id}
```

### Update alert

```
PUT /api/v1/alerts/{id}
```

### Delete alert

```
DELETE /api/v1/alerts/{id}
```

Response `204`. `404` if not found.

---

## Stored Procedures

Stored procedures define reusable SQL logic that is managed in the config database and deployed to the data database. See [workflows.md](workflows.md) for the full lifecycle, admin UI, and usage within workflows.

### List stored procedures

```
GET /api/v1/stored-procedures
```

Response `200`:
```json
[
  {
    "id": 1,
    "name": "get_entity_counts",
    "description": "Returns row counts for config tables",
    "sql_text": "DROP PROCEDURE IF EXISTS get_entity_counts;\nCREATE PROCEDURE ...",
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-01T10:00:00Z"
  }
]
```

### Create stored procedure

```
POST /api/v1/stored-procedures
```

```json
{
  "name": "get_entity_counts",
  "description": "Returns row counts for config tables",
  "sql_text": "DROP PROCEDURE IF EXISTS get_entity_counts;\nCREATE PROCEDURE get_entity_counts(IN min_count INT)\nBEGIN\n  SELECT 'orgs' AS entity_type, COUNT(*) AS count FROM orgs;\nEND"
}
```

Required fields: `name`, `sql_text`. Response `201`.

### Get stored procedure

```
GET /api/v1/stored-procedures/{id}
```

### Update stored procedure

```
PUT /api/v1/stored-procedures/{id}
```

```json
{
  "name": "get_entity_counts",
  "description": "Updated description",
  "sql_text": "DROP PROCEDURE IF EXISTS ..."
}
```

### Delete stored procedure

```
DELETE /api/v1/stored-procedures/{id}
```

Response `204`. `404` if not found.

### Deploy stored procedure

```
POST /api/v1/stored-procedures/{id}/deploy
```

Executes the procedure's `sql_text` against the **data database**, creating or replacing the procedure. Response `200`:

```json
{"ok": true}
```

On failure: `{"error": "SQL syntax error ..."}`.

### Execute SQL (test)

```
POST /api/v1/stored-procedures/execute-sql
```

Runs arbitrary SQL against the data database for testing purposes.

```json
{"sql": "CALL get_entity_counts(0)"}
```

Response `200`:
```json
{"output": [{"entity_type": "orgs", "count": 1}]}
```

On failure: `{"error": "..."}`.

---

## Canned Queries

Reusable SQL query templates for the workflow editor library.

### List canned queries

```
GET /api/v1/canned-queries
```

### Create canned query

```
POST /api/v1/canned-queries
```

```json
{
  "name": "Config Entity Counts",
  "description": "Returns the number of rows in each core configuration table.",
  "category": "Admin",
  "sql_text": "CALL get_entity_counts(0)",
  "parameters": [],
  "version": 1
}
```

### Get / Update / Delete

```
GET    /api/v1/canned-queries/{id}
PUT    /api/v1/canned-queries/{id}
DELETE /api/v1/canned-queries/{id}
```

---

## Canned Scripts

Reusable JavaScript fragments for the workflow editor library.

### List canned scripts

```
GET /api/v1/canned-scripts
```

### Create canned script

```
POST /api/v1/canned-scripts
```

```json
{
  "name": "Rows to Keyed Object",
  "description": "Converts [{entity_type, count}] rows to {entity_type: count}",
  "category": "Transforms",
  "js_text": "var obj = {}; ...",
  "version": 1
}
```

### Get / Update / Delete

```
GET    /api/v1/canned-scripts/{id}
PUT    /api/v1/canned-scripts/{id}
DELETE /api/v1/canned-scripts/{id}
```

---

## API Keys (Admin Only)

These endpoints require an admin API key. Regular API keys receive `403 Forbidden`.

### List keys

```
GET /api/v1/keys
```

Response `200`:
```json
[
  {
    "id": 1,
    "name": "bootstrap-admin",
    "is_admin": true,
    "created_at": "2025-01-15T10:00:00Z"
  },
  {
    "id": 2,
    "name": "ci-automation",
    "is_admin": false,
    "created_at": "2025-01-16T14:30:00Z"
  }
]
```

The key value itself is never returned after creation.

### Create key

```
POST /api/v1/keys
```

```json
{
  "name": "ci-automation",
  "is_admin": false
}
```

Response `201`:
```json
{
  "id": 2,
  "name": "ci-automation",
  "key": "<64 hex characters>",
  "is_admin": false
}
```

**The `key` field is shown exactly once.** Store it securely; it cannot be retrieved again.

### Delete key

```
DELETE /api/v1/keys/{id}
```

Response `204`. `404` if not found.

---

## DevLake Data Database (Admin)

### Run a SQL query

```
POST /api/v1/devlake-db/query
```

**Admin only.** Executes a raw SQL query against the DevLake data database. Used by the admin SQL client in the UI.

Request:
```json
{"sql": "SELECT id, name FROM repos LIMIT 10"}
```

Response `200`:
```json
{
  "columns": ["id", "name"],
  "rows": [["github:GithubRepo:1:123", "myorg/myrepo"]]
}
```

### List tables

```
GET /api/v1/devlake-db/tables
```

**Admin only.** Returns all table names in the DevLake data database.

Response `200`:
```json
["boards", "cicd_pipelines", "commits", "issues", "pull_requests", "repos", ...]
```

---

## Bootstrapping

> **Note:** API key bootstrapping is **optional** and useful for headless or scripted access. Browser users can authenticate through a trusted proxy; local development can use `AUTH_TEST_MODE` or `DEV_ADMIN_OVERRIDE`.

### Option 1: `make create-admin-key` (recommended)

Generate an admin key directly in the database without starting the sidecar:

```bash
export DASHBOARD_DB_HOST=localhost DASHBOARD_DB_PORT=3306
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
make create-admin-key
```

This connects to the database (which must already be migrated — see [migrations.md](migrations.md)), generates a random admin API key, and prints it to stdout. Save the key — it cannot be retrieved later.

### Option 2: `DASHBOARD_ADMIN_API_KEY` environment variable

Set `DASHBOARD_ADMIN_API_KEY` before starting the sidecar to bootstrap a key with a value you choose:

```bash
export DASHBOARD_ADMIN_API_KEY=$(openssl rand -hex 32)
echo "Admin key: $DASHBOARD_ADMIN_API_KEY"  # save this!
./bin/orgpulse
```

The sidecar inserts the key on startup (idempotent — safe to leave set on subsequent runs).

### Creating additional keys

Once you have an admin key, create additional keys via the API:

```bash
curl -s -X POST http://localhost:9090/api/v1/keys \
  -H "Authorization: Bearer $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "ci-automation", "is_admin": false}' | jq .
```

Or use the admin UI at `/admin/` to manage keys in the browser.

---

## Importing Configuration

Use `make import-config` to bulk-import configuration from JSON files into the database. This is intended as a migration tool for loading data from an existing setup or hand-written config.

### Directory structure

```
data/
  orgs/
    myorg.json
  products/
    prod-a.json
    prod-b.json
  teams/
    team-1.json
    team-2.json
```

Each `.json` file contains a single entity object in the assembled JSON format (the same format returned by `/api/orgs`, `/api/products`, `/api/teams` and defined by the JSON schemas in `schemas/`). Missing subdirectories are silently skipped.

### Example org file (`orgs/myorg.json`)

```json
{
  "id": "myorg",
  "name": "My Organization",
  "products": ["prod-a", "prod-b"],
  "dashboards": [
    {
      "id": "org-overview",
      "type": "baseorgmetrics",
      "title": "Org Overview",
      "diagrams": [
        {"position": 0, "type": "json", "endpoint": "https://metrics.example.com/org-metrics", "span": 2, "title": "Summary"}
      ]
    }
  ]
}
```

### Example product file (`products/prod-a.json`)

```json
{
  "id": "prod-a",
  "name": "Product A",
  "contactemail": "owner@example.com",
  "teams": ["team-1", "team-2"],
  "config": {
    "cycleTimeSlowHours": 168,
    "cycleTimeCriticalHours": 336,
    "analyticsprompt": "Focus on deployment frequency.",
    "overrides": {
      "sources": {
        "repos": [
          {"id": "shared-repo", "type": "github", "owner": "my-org", "name": "shared-repo", "teams": ["team-1", "team-2"]}
        ]
      }
    }
  }
}
```

### Example team file (`teams/team-1.json`)

```json
{
  "id": "team-1",
  "name": "Team One",
  "contactemail": "team@example.com",
  "product": ["Product A"],
  "fetchurl": "https://devlake/api/blueprints/{{blueprintid}}/trigger",
  "fetchcheckurl": "https://devlake/api/blueprints/{{blueprintid}}/pipelines",
  "sources": [
    {
      "id": "gitrepos",
      "type": "gitrepos",
      "repos": [
        {"id": "my-repo", "type": "github", "owner": "my-org", "name": "my-repo", "excludedDashboardTypes": ["codecoverage"], "whitelist": ["user1"]}
      ]
    },
    {
      "id": "jira-source",
      "type": "workitems",
      "projects": [
        {"id": "aap", "projectname": "AAP Project", "connectionid": "1", "jiraproject": "AAP", "type": "jira"}
      ]
    }
  ],
  "dashboards": [
    {
      "id": "pr-cycletime",
      "type": "prcycletime",
      "title": "PR Cycle Time",
      "blueprintids": ["bp-123"],
      "defaultsource": "gitrepos",
      "connectionid": "1",
      "diagrams": [
        {"position": 0, "type": "json", "endpoint": "https://metrics.example.com/cycletime", "span": 2, "title": "Cycle Time Trend", "aianalytics": true}
      ]
    }
  ]
}
```

### Running the import

```bash
export DASHBOARD_DB_USER=metricsconfig DASHBOARD_DB_PASSWORD=changeme DASHBOARD_DB_NAME=dashboard
make import-config DIR=path/to/data
```

The tool imports in foreign-key-safe order: teams first, then products, then orgs. Each entity and all its nested data (sources, repos, dashboards, diagrams, config, overrides, relationships) are created in a single run.

After importing, verify the result using the assembled JSON endpoints. These require an authenticated caller and return only that caller's grant:

```bash
curl -s http://localhost:9090/api/orgs | jq .
curl -s http://localhost:9090/api/products | jq .
curl -s http://localhost:9090/api/teams | jq .
```

---

## Report Generation

### Send report

```
POST /sendreport
```

Triggers a subscription-based report pipeline. Requires an admin (admin API key or a user in `ADMIN_USERS`) and the report service to be configured (see README for required environment variables). The scheduler sends due reports on its own; this endpoint is for sending one on demand.

```json
{"subscription_id": 1}
```

**Pipeline steps:**

1. Looks up the subscription by ID
2. For each subscription element: POSTs the element URL with its payload, renders a chart from the response, and (if the subscription has an `ai_analytics_url`) sends the response JSON to that endpoint for per-element analysis
3. Assembles a partial HTML report from all elements
4. Sends the partial HTML and all raw JSON responses to the AI summary endpoint for an executive summary
5. Assembles the final HTML report with the executive summary on top
6. Sends the report via email to the subscription's email address

**Responses:**

| Status | Meaning |
|--------|---------|
| `200` | `{"status": "sent"}` — report generated and emailed |
| `400` | Missing `subscription_id` |
| `401` / `403` | Not authenticated / not an admin |
| `404` | Subscription not found |
| `500` | Error during report generation (metrics fetch, AI endpoint, chart rendering, or SMTP) |
| `503` | Report service not configured (missing environment variables) |

### Render chart

```
POST /chart
```

Renders a Chart.js-compatible JSON payload as a PNG image. Returns `image/png` with the rendered chart. Requires authentication (any API key or signed-in user). This endpoint is standalone and does not require the report service.

---

## Workflows

Workflow CRUD, versioning, test execution, and live endpoints are documented in [workflows.md](workflows.md). The workflow API includes ~20 endpoints under `/api/v1/workflows/` (all require auth) plus the live execution endpoints at `GET/POST /workflows/staging/{name}` (local) and `GET/POST /workflows/production/{name}` (proxied to the workflow execution service). Live execution requires the same authentication as the rest of the API. A non-admin caller must name repositories or work-item projects that belong to a granted team; a body with neither is rejected.

## DevLake Integration

DevLake onboarding, sync, connections, and proxy endpoints are documented in [devlake-integration.md](devlake-integration.md).

## Automation Tips

1. Send the API key in the `X-API-Key` header (or `Authorization: Bearer <key>`) with every request.
2. Use the base URL of the dashboard service (e.g., `http://devlake-dashboard:9090`).
3. For creating a full team setup, chain these requests:
   - `POST /api/v1/teams` → create the team
   - `POST /api/v1/teams/{id}/sources` → add sources with repos
   - `POST /api/v1/dashboards` → add dashboards with diagrams
   - `PUT /api/v1/products/{id}/teams` → link team to product
4. Use the assembled JSON endpoints (`GET /api/orgs`, `/api/products`, `/api/teams`) to verify the result — these return the full nested structure that the dashboard frontend consumes.
