# Workflows

Workflows are named data pipelines that expose HTTP endpoints. Each workflow follows a three-stage execution model:

1. **Input Transform** — a JavaScript function that validates and reshapes incoming parameters.
2. **SQL Queries** — one or more parameterized SQL statements executed in parallel against the data lake.
3. **Output Transform** — a JavaScript function that combines query results into the final response.

Published workflows are available at `GET/POST /workflows/staging/<name>` (local execution) and `GET/POST /workflows/production/<name>` (proxied to the workflow execution service). These endpoints require authentication and enforce the caller's data scope.

## Data Model

### Workflow (API response)

| Field | Type | Storage | Description |
|-------|------|---------|-------------|
| `id` | int | column | Auto-generated primary key |
| `name` | string | column | URL-safe endpoint name (unique) |
| `description` | string | column | Human-readable description |
| `input_script` | string | column | JavaScript — input transformation |
| `output_script` | string | column | JavaScript — output transformation |
| `test_input` | string | column | JSON — default test parameters |
| `tests` | array | column (JSON) | Test cases (name, input JSON, assert script, optional schema_id) |
| `version` | int | column | Publication version (0 = unpublished draft) |
| `has_draft` | bool | computed | `TRUE` when the `draft` column is not NULL |
| `production_version` | int | computed | Version currently serving live traffic (0 = none). **Not a column on the `workflows` table.** Derived via subquery: `SELECT version FROM workflow_versions WHERE workflow_id = w.id AND production = TRUE` |
| `archived` | bool | column | Whether the workflow is archived (disabled) |
| `queries` | array | joined | Ordered list of SQL query definitions (from `workflow_queries` table) |

> **Important:** `production_version` and `has_draft` are not stored as columns on the `workflows` table. If you query the database directly, compute them from the `workflow_versions` table and the `draft` column respectively. See the Database Schema section below.

### Database Schema

The `workflows` table columns (after all migrations):

```sql
id, name, description, input_script, output_script, test_input, tests,
archived, draft, version, created_at, updated_at
```

The `workflow_versions` table columns:

```sql
id, workflow_id, version, name, description, input_script, output_script,
test_input, queries (JSON), tests, publish_note, production (BOOLEAN),
published_at
```

The production version is tracked by the `production` boolean on `workflow_versions`:

```sql
SELECT version FROM workflow_versions
WHERE workflow_id = ? AND production = TRUE
```

To load a production workflow version with its full definition:

```sql
SELECT id, workflow_id, version, name, description, input_script,
       output_script, test_input, queries, tests, publish_note,
       production, published_at
FROM workflow_versions
WHERE workflow_id = ? AND production = TRUE
```

> **Note:** The workflow definition fields (`input_script`, `output_script`, `queries`, etc.) are stored directly as columns on `workflow_versions` — there is no `data` column. The `queries` column is JSON containing the array of query definitions.

### Workflow Query

| Field | Type | Description |
|-------|------|-------------|
| `stage` | int | Execution step (0-based). Queries in the same step run in parallel; steps execute sequentially. |
| `sort_order` | int | Display order within a step |
| `label` | string | Key used to reference results in the output script |
| `sql_text` | string | SQL statement (use `:variable_name` for named parameter placeholders) |

### JSON Schema

| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Auto-generated primary key |
| `name` | string | Schema name (unique) |
| `description` | string | Human-readable description |
| `json_schema` | string | JSON Schema document |
| `version` | int | Auto-incremented on update |

JSON Schemas are a global catalog of reusable schemas that can be referenced by workflow test cases for output validation. Managed via the **JSON Schemas** tab in the admin UI.

### Stored Procedure

| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Auto-generated primary key |
| `name` | string | Procedure name (unique) |
| `description` | string | Human-readable description |
| `sql_text` | string | Full `CREATE PROCEDURE` SQL statement |

## Execution Pipeline

```
                     ┌─────────────────────┐
  HTTP request  ───▶ │  Input Transform    │ ───▶ map of parameters
  (JSON body         │  (JavaScript/goja)  │
   or query params)  └─────────────────────┘
                              │
                     ┌────────┴────────┐
                     │    Step 1       │
              ┌──────┼──────┐         │
              ▼      ▼      ▼         │  (parallel within step)
           Query A Query B Query C    │
              │      │      │         │
              └──────┼──────┘         │
                     ├────────────────┘
                     │    Step 2
              ┌──────┼──────┐            (sequential between steps)
              ▼      ▼      ▼
           Query D Query E Query F
              │      │      │
              └──────┼──────┘
                     ▼
                     ┌─────────────────────┐
                     │  Output Transform   │ ───▶ HTTP response
                     │  (JavaScript/goja)  │
                     └─────────────────────┘
```

### Input Transform

The input script receives a global `input` object containing the request parameters. It must return an object whose keys are used to resolve query parameters.

```javascript
// Extract and validate team_id from request params
var tid = input.team_id || '';
if (!tid) throw new Error('team_id is required');
({team_id: tid})
```

When a dashboard calls a workflow, `input` is the dashboard's standardized request body. It can also contain extra parameters. Some are hardcoded per dashboard type and read from the team's config (for example `rfe_station_id` on `aisdlc` dashboards). Others come from the diagram's `params` (for example `view`). See [Dashboard request parameters](rest-api.md#dashboard-request-parameters). Input transforms should ignore keys they don't use.

### SQL Queries

Queries are organized into **steps**. All queries within the same step execute in **parallel**. Steps execute **sequentially** from left to right — step 1 completes before step 2 begins. This allows later steps to depend on data populated by earlier ones.

Each query defines a `stage` (step number), a `label`, and a `sql_text` with named parameter placeholders. Named parameters use the `:variable_name` syntax and are resolved from the input transform output and prior query results.

```sql
-- stage: 0, label: team_info
SELECT id, name FROM teams WHERE id = :team_id
```

Queries can also call stored procedures:

```sql
-- stage: 0, label: entity_counts
CALL get_entity_counts(:min_count)
```

The same variable can appear multiple times in a query:

```sql
SELECT * FROM events WHERE start_date >= :from AND end_date <= :from
```

If all queries are independent, a single step (stage 0) is sufficient and they all run in parallel. Use multiple steps when a query needs data produced by a prior step.

### Named Parameter Resolution

Named parameters (`:variable_name`) in SQL are resolved at execution time. The resolver builds a scope by merging **prior query results** (keyed by query label) with the **input transform output** (which takes precedence on key collisions). Each `:variable_name` is replaced with a `?` placeholder and the corresponding value is bound as a positional argument, keeping queries parameterized and safe from SQL injection.

**Variable names** must start with a letter or underscore, followed by word characters: `:team_id`, `:min_count`, `:_internal`. A bare `:123` is not matched.

**Repeated variables** — the same variable can appear multiple times. Each occurrence generates its own `?` and arg:

```sql
WHERE created >= :from AND closed <= :from
-- resolves to: WHERE created >= ? AND closed <= ?
-- args: [from_value, from_value]
```

**Slice expansion** — if a variable resolves to an array/slice, it expands into multiple comma-separated `?` placeholders. This is designed for `IN` clauses:

```sql
WHERE r.name IN (:repos)
-- if repos = ["repo-a", "repo-b", "repo-c"]
-- resolves to: WHERE r.name IN (?, ?, ?)
-- args: ["repo-a", "repo-b", "repo-c"]
```

Empty slices bind a single `NULL`.

**Comments and string literals are skipped** — the resolver does not match `:variable_name` inside:

- Line comments (`-- ...`)
- Block comments (`/* ... */`)
- Single-quoted string literals (`'...'`)

This means you can safely comment out parameterized clauses:

```sql
WHERE status = :status
  -- AND author IN (:user_whitelist)
  AND created >= :from
```

Only `:status` and `:from` are resolved; `:user_whitelist` inside the comment is left untouched.

### Output Transform

The output script receives two globals:

- `input` — the output of the input transform
- `results` — an object keyed by query label, each value an array of row objects

```javascript
var info = results['team_info'] || [];
var team = info.length > 0 ? info[0] : null;
({team: team})
```

### Console

All scripts have access to `console.log()`, `console.info()`, `console.warn()`, `console.error()`, and `console.debug()`. Output is captured and returned in the test response (see Test Execution below) but is not included in live endpoint responses.

## Stored Procedures

Stored procedures let you encapsulate reusable SQL logic in the database. They are managed through the admin UI and can be called from workflow queries using `CALL procedure_name(:param)`.

### Lifecycle

1. **Create** — write the procedure definition (a full `CREATE PROCEDURE` SQL statement) in the stored procedure editor.
2. **Save** — auto-saved to the config database on edit. This does not create the procedure in the data database.
3. **Deploy to Production** — explicitly deploys the SQL definition to the production data database (requires confirmation). The procedure is live immediately for all workflows that call it.
4. **Use** — reference the procedure in workflow queries with `CALL procedure_name(:param)`.

> **Warning:** Deploying a stored procedure overwrites the live version in the production database. The test panel is disabled while you have undeployed changes — deploy first to test against the current database state.

### Writing Procedures

Use `DROP PROCEDURE IF EXISTS` followed by `CREATE PROCEDURE` to make definitions idempotent:

```sql
DROP PROCEDURE IF EXISTS get_entity_counts;
CREATE PROCEDURE get_entity_counts(IN min_count INT)
BEGIN
  SELECT entity_type, count FROM (
    SELECT 'orgs' AS entity_type, COUNT(*) AS count FROM orgs
    UNION ALL SELECT 'products', COUNT(*) FROM products
    UNION ALL SELECT 'teams', COUNT(*) FROM teams
  ) counts
  WHERE count >= min_count
  ORDER BY count DESC;
END
```

### Using Procedures in Workflows

In the workflow SQL query editor, call the procedure like any other SQL statement:

```sql
CALL get_entity_counts(:min_count)
```

Named parameter placeholders are resolved from the input transform output the same way as regular queries.

### Example: Config Summary Workflow

This workflow uses the `get_entity_counts` stored procedure instead of inline SQL:

**Input Transform:**
```javascript
var min = parseInt(input.min_count) || 0;
({min_count: min})
```

**SQL Query:**
- Label: `entity_counts`
- SQL: `CALL get_entity_counts(:min_count)`

**Output Transform:**
```javascript
var counts = results['entity_counts'] || [];
var summary = {};
for (var i = 0; i < counts.length; i++) {
  summary[counts[i].entity_type] = counts[i].count;
}
({summary: summary})
```

### Testing Procedures

The stored procedure editor includes a **Test** panel where you can run arbitrary SQL against the data database:

```sql
CALL get_entity_counts(0);
CALL get_team_sources('test-team');
SHOW PROCEDURE STATUS WHERE Db = DATABASE();
```

### REST API

All endpoints require API key authentication.

```
GET    /api/v1/stored-procedures              — list all
POST   /api/v1/stored-procedures              — create
GET    /api/v1/stored-procedures/{id}          — get one
PUT    /api/v1/stored-procedures/{id}          — update
DELETE /api/v1/stored-procedures/{id}          — delete
POST   /api/v1/stored-procedures/{id}/deploy   — deploy to data database
POST   /api/v1/stored-procedures/execute-sql   — run arbitrary SQL for testing
```

**Deploy** sends the procedure's `sql_text` to the data database. Returns `{"ok": true}` on success or `{"error": "..."}` on failure.

**Execute SQL** runs any SQL statement against the data database:
```json
{"sql": "CALL get_entity_counts(0)"}
```
Response: `{"output": [{"entity_type": "orgs", "count": 1}, ...]}` or `{"error": "..."}`.

## Draft / Publish Lifecycle

Workflows use a draft/publish model:

1. **Auto-save** — edits are saved as a draft (`PUT /api/v1/workflows/{id}/draft`) without affecting the live endpoint.
2. **Publish** — promotes the draft to a new staging version (`POST /api/v1/workflows/{id}/publish`), increments the version number, and clears the draft. The new version is not yet serving live traffic.
3. **Set Production** — marks a specific version for live use (`POST /api/v1/workflows/{id}/versions/{version}/production`). If the version has test cases, all tests must pass before promotion is allowed.
4. **Version history** — each publish snapshots the workflow (including test cases) into the `workflow_versions` table.
5. **Live endpoints** — `GET/POST /workflows/staging/{name}` serves the production version via local execution (falls back to latest published if no production version is set). `GET/POST /workflows/production/{name}` proxies to the cluster-internal workflow execution service.

A workflow with `version = 0` has never been published and is not accessible via the live endpoint.

## Archiving

Workflows can be archived to disable them without deleting:

1. **Archive** — `POST /api/v1/workflows/{id}/archive` clears the production version and sets `archived = true`. The workflow is excluded from the live endpoint.
2. **Unarchive** — `POST /api/v1/workflows/{id}/unarchive` sets `archived = false` but does **not** restore the production version. The user must manually set a production version to re-enable live serving.

Archived workflows remain visible in the admin UI under the "Archived" tab and can still be edited.

## Export / Import

Workflows can be exported to a self-contained JSON bundle and imported on another instance. Both endpoints require admin access.

The three Code Coverage bundles are versioned here as portable workflow definitions: [key metrics](../workflows/code-coverage/coveragekeymetrics.json), [trend](../workflows/code-coverage/coveragetrend.json), and [line breakdown](../workflows/code-coverage/coveragelinebreakdown.json). Workflows execute from database records, not these files; building or deploying the dashboard does **not** import them. Older workflows, including PR Cycle Time, are already managed in the workflow database and do not have source bundles in this repository. Key Metrics uses Codecov coverage and configuration data; its bypass rate and bypassed-PR details additionally require DevLake GitHub pull-request data for the selected repositories. Trend uses Codecov daily coverage and flags; its patch-coverage series requires matched merged GitHub pull requests.

### Rolling out Code Coverage workflows

1. Ensure the target DevLake instance has collected Codecov data for the selected team repositories. The Codecov connection and blueprint are configured separately from workflow import; see [Codecov setup](devlake-integration.md#codecov-setup-in-the-dashboard).
2. With a dashboard **admin** API key, import each bundle into the target dashboard using `POST /api/v1/workflows/import` with `Content-Type: application/json` and the bundle as the request body. The response gives the new workflow ID. Import creates a database record but does not publish it. If a workflow with the same name already exists, import returns `409`; edit that workflow's draft through the VS Code extension or workflow API instead of re-importing or deleting it.
3. Run the workflow test cases against the target DevLake database (`POST /api/v1/workflows/{id}/run-tests`). The bundled positive test cases use specific repositories and historical dates, so select equivalent test data if those repositories are not collected in the target instance. Check the three chart responses for one repository, multiple repositories, missing data, and applicable flags. Publish each verified workflow (`POST /api/v1/workflows/{id}/publish`) and set the published version as production (`POST /api/v1/workflows/{id}/versions/{version}/production`).
4. Confirm that `POST /workflows/staging/{name}` works on the dashboard and `POST /workflows/production/{name}` works through its `WORKFLOW_EXEC_URL` proxy. The latter calls the separate workflow execution service; if it cannot find the new definitions, check that service's workflow-store configuration before changing dashboard URLs.
5. Deploy the dashboard image containing the updated Code Coverage template, then verify the three charts in a Codecov-enabled team. The template changes **new** dashboard configurations only. For an existing team, update its stored Code Coverage diagram endpoints to `/workflows/production/coveragekeymetrics`, `/workflows/production/coveragetrend`, and `/workflows/production/coveragelinebreakdown` through the dashboard editor or `PUT /api/v1/dashboards/{auto_id}` (which replaces all diagrams and blueprint IDs). Check the saved dashboard record before editing so its other settings are preserved.

Promoting workflow versions and updating existing team dashboards changes environment database state; neither happens automatically when the MR is merged.

### Export

```
GET /api/v1/workflows/{id}/export
```

Returns a JSON bundle containing the complete workflow definition, its stored procedures, referenced JSON schemas, and version history. All internal IDs are stripped so the bundle is portable across environments.

Response `200`:
```json
{
  "format_version": 1,
  "exported_at": "2024-01-15T10:30:00Z",
  "workflow": {
    "name": "config-summary",
    "description": "Returns entity counts",
    "input_script": "({min_count: parseInt(input.min_count) || 0})",
    "output_script": "({summary: results.entity_counts})",
    "test_input": "{\"min_count\": 0}",
    "tests": [
      {
        "name": "Returns counts",
        "input": "{\"min_count\": 0}",
        "assert_script": "if (!output.summary) throw 'missing summary'",
        "schema_id": 1
      }
    ],
    "version": 3,
    "queries": [
      {"stage": 0, "sort_order": 0, "label": "entity_counts", "sql_text": "CALL get_entity_counts(:min_count)"}
    ]
  },
  "stored_procedures": [
    {
      "name": "get_entity_counts",
      "description": "Returns row counts for config tables",
      "sql_text": "DROP PROCEDURE IF EXISTS get_entity_counts; CREATE PROCEDURE ..."
    }
  ],
  "json_schemas": [
    {
      "name": "config-summary-output",
      "description": "Validates config-summary output",
      "json_schema": "{\"type\":\"object\",\"required\":[\"summary\"]}"
    }
  ],
  "versions": []
}
```

The `Content-Disposition` header is set to suggest a filename based on the workflow name.

Response `404`: workflow not found.

### Import

```
POST /api/v1/workflows/import
```

Imports a workflow from an export bundle. Creates the workflow and all associated resources.

**Behavior:**
- **Workflow** — created as a new workflow. Returns `409` if a workflow with the same name already exists.
- **JSON Schemas** — matched by name. If a schema with the same name already exists, the existing schema is reused (not overwritten). New schemas are created. Schema IDs in test cases are remapped automatically.
- **Stored Procedures** — created with the new workflow ID. Duplicate names within the same workflow are skipped with a warning.
- **Versions** — not imported (the workflow starts fresh at its bundle version number).

Request body: the JSON bundle from the export endpoint.

Response `201`:
```json
{
  "workflow_id": 42,
  "workflow_name": "config-summary",
  "imported": {
    "stored_procedures": 1,
    "json_schemas": 1
  }
}
```

The `warnings` field appears if any stored procedures were skipped:
```json
{
  "workflow_id": 42,
  "workflow_name": "config-summary",
  "imported": {"stored_procedures": 0, "json_schemas": 1},
  "warnings": ["stored procedure \"get_entity_counts\" already exists, skipped"]
}
```

Response `400`: invalid bundle (bad JSON, unsupported format version, missing required fields).

Response `409`: workflow with the same name already exists.

## Editor Locking

The admin UI uses an in-memory lock to prevent concurrent editing of the same workflow by multiple browser sessions.

- **Acquire** — `POST /api/v1/workflows/{id}/lock` with `{"session_id": "..."}`. Returns `200` if acquired or renewed, `409` if held by another session.
- **Release** — `DELETE /api/v1/workflows/{id}/lock` with `{"session_id": "..."}`.
- **Heartbeat** — the frontend re-acquires every 10 seconds. Locks expire after 30 seconds of inactivity.

Each browser tab generates a unique session ID (`crypto.randomUUID()`), so two tabs using the same API key are treated as separate sessions. The MCP server generates its own session ID on startup (`mcp-<hex>`).

Locks are stored in server memory and are cleared on server restart.

## REST API

All `/api/v1/*` endpoints require authentication. The live endpoints `/workflows/staging/{name}` and `/workflows/production/{name}` do too. A caller who is not an admin or operator must send `owner`/`name` and `projects` values that all belong to a team in their Rover grant. A request with no repository and no work-item project is rejected, so a query cannot run unbounded. Workflow-definition export stays admin-only.

### List workflows

```
GET /api/v1/workflows
```

Response `200`:
```json
[
  {
    "id": 1,
    "name": "config-summary",
    "description": "Returns entity counts using stored procedure",
    "version": 2,
    "has_draft": false,
    "queries": [{"label": "entity_counts", "sql_text": "CALL get_entity_counts(:min_count)"}]
  }
]
```

### Create workflow

```
POST /api/v1/workflows
```

```json
{
  "name": "my-workflow",
  "description": "...",
  "input_script": "({});",
  "output_script": "(results);",
  "test_input": "{}",
  "queries": [
    {"sort_order": 0, "label": "data", "sql_text": "CALL my_procedure(:param1)"}
  ]
}
```

Response `201`: the created workflow object with `version: 0`.

### Get workflow

```
GET /api/v1/workflows/{id}
```

Returns the draft version if a draft exists, otherwise the published version. The `has_draft` field indicates which.

### Update workflow

```
PUT /api/v1/workflows/{id}
```

Directly updates the published fields (bypasses the draft system). Typically used for initial setup; prefer draft/publish for iterative editing.

### Delete workflow

```
DELETE /api/v1/workflows/{id}
```

Response `204`. Also deletes all associated queries and version history.

### Save draft

```
PUT /api/v1/workflows/{id}/draft
```

Body: full `WorkflowRow` JSON. Saves the entire state as a draft without modifying the published version.

### Publish

```
POST /api/v1/workflows/{id}/publish
```

Promotes the current draft (or current published state if no draft) to a new version. Inserts a snapshot into `workflow_versions`.

Response `200`: the updated workflow with incremented `version`.

### Test execution

```
POST /api/v1/workflows/{id}/test
```

Body: JSON object with input parameters.

Response `200`:
```json
{
  "stages": [
    {"stage": "input_script", "output": {"team_id": "my-team"}},
    {"stage": "query:0:team_info", "output": [{"id": "my-team", "name": "My Team"}]},
    {"stage": "query:0:team_sources", "output": [{"id": "src-1", "type": "gitrepos"}]},
    {"stage": "output_script", "output": {"team": {"id": "my-team", "name": "My Team"}}}
  ],
  "output": {"team": {"id": "my-team", "name": "My Team"}},
  "logs": [
    {"level": "log", "message": "processing team", "stage": "input_script"}
  ]
}
```

If a stage fails, its entry includes an `error` field and execution stops. The top-level `error` field summarizes which stage failed.

### Run single stage

```
POST /api/v1/workflows/run-stage
```

Runs a single JavaScript or SQL fragment in isolation, used for iterative debugging.

**JavaScript:**
```json
{
  "type": "js",
  "script": "var x = input.count * 2; ({doubled: x})",
  "globals": {"input": {"count": 5}}
}
```

Response: `{"output": {"doubled": 10}, "logs": [...]}`

**SQL:**
```json
{
  "type": "sql",
  "script": "CALL get_entity_counts(:min_count)",
  "scope": {"min_count": 0}
}
```

Response: `{"output": [{"entity_type": "orgs", "count": 1}, ...]}`

Errors are returned in the response body as `{"error": "..."}` with HTTP 200.

### Run tests

```
POST /api/v1/workflows/{id}/run-tests
```

Runs all test cases defined on the workflow. Each test case's input is fed through the full execution pipeline. If a `schema_id` is set, the output is validated against the referenced JSON Schema before assertion scripts run. Then the assertion script is evaluated server-side via goja.

Response `200`:
```json
{
  "results": [
    {"name": "Returns summary", "passed": true},
    {"name": "Rejects bad input", "passed": false, "error": "expected pipeline to fail"}
  ]
}
```

### JSON Schemas (CRUD)

```
GET    /api/v1/json-schemas          — list all schemas
POST   /api/v1/json-schemas          — create a schema
GET    /api/v1/json-schemas/{id}     — get a schema
PUT    /api/v1/json-schemas/{id}     — update a schema
DELETE /api/v1/json-schemas/{id}     — delete a schema
```

Create/update body:
```json
{
  "name": "team-details-output",
  "description": "Validates team details workflow output",
  "json_schema": "{\"type\":\"object\",\"required\":[\"team\"],\"properties\":{\"team\":{\"type\":\"object\"}}}"
}
```

The `json_schema` field must be a valid JSON string containing a JSON Schema document. Supported keywords: `type`, `properties`, `required`, `items`, `enum`, `minimum`, `maximum`, `minLength`, `maxLength`, `pattern`, `additionalProperties`.

### Export workflow

```
GET /api/v1/workflows/{id}/export
```

Returns a self-contained JSON bundle. See [Export / Import](#export--import) above.

### Import workflow

```
POST /api/v1/workflows/import
```

Imports a workflow from an export bundle. See [Export / Import](#export--import) above.

### Archive workflow

```
POST /api/v1/workflows/{id}/archive
```

Clears the production version and marks the workflow as archived. Returns `200 {"ok": true}`.

### Unarchive workflow

```
POST /api/v1/workflows/{id}/unarchive
```

Clears the archived flag. Does **not** restore any production version. Returns `200 {"ok": true}`.

### Lock workflow

```
POST /api/v1/workflows/{id}/lock
```

Body: `{"session_id": "unique-session-uuid"}`

Acquires or renews an editor lock. Returns `200 {"ok": true}` on success, `409 {"error": "locked", "locked_session": "..."}` if held by another session. Locks expire after 30 seconds without renewal.

### Unlock workflow

```
DELETE /api/v1/workflows/{id}/lock
```

Body: `{"session_id": "unique-session-uuid"}`

Releases the lock if held by the given session. Returns `200 {"ok": true}`.

### Live endpoints

#### Staging (local execution)

```
GET  /workflows/staging/{name}?param1=value1&param2=value2
POST /workflows/staging/{name}   (JSON body)
```

Requires authentication and enforces the caller's data scope. Only published workflows (`version > 0`) are served. Executes the workflow locally in the dashboard process.

- **GET**: query parameters are passed as the input object (single values as strings, repeated keys as arrays).
- **POST**: the JSON body is passed as the input object.

Response `200`: the output transform result directly (not wrapped in stages).

Response `404`: workflow not found or not published.

Response `500`: execution error with `{"error": "..."}`.

#### Production (proxied)

```
GET  /workflows/production/{name}?param1=value1&param2=value2
POST /workflows/production/{name}   (JSON body)
```

Requires authentication and enforces the caller's data scope. Proxies the request to the workflow execution service (`WORKFLOW_EXEC_URL`). The upstream path is `/workflows/{name}`.

Response codes and format are determined by the upstream service.

## Admin UI

The workflow editor described below belongs to the earlier web UI. The current PatternFly admin UI does not expose a Workflows page. Use the VS Code extension for draft editing and the authenticated workflow API for import, tests, publishing, and promotion until that page is restored.

### Workflow List

The Workflows section has two tabs:

- **Active** — shows non-archived workflows. Each row displays: ID, name, description, staging version badge, production version badge, last updated timestamp.
- **Archived** — shows archived workflows with the same columns.

Both tabs share a **search box** that filters by name and description.

**Action buttons per row (Active tab):**
- **History** — opens the version history modal with the ability to set a production version directly from the list.
- **Edit** — opens the workflow editor (acquires a lock first).
- **Archive** — archives the workflow after confirmation (clears production version).
- **Delete** — deletes the workflow after confirmation.

**Action buttons per row (Archived tab):**
- **Unarchive** — moves the workflow back to Active without setting a production version.
- **Edit** — opens the workflow editor.
- **Delete** — deletes the workflow.

A lock icon appears next to the workflow name when it is locked (amber for another session, green for the current session).

### Workflow Editor Layout

The editor is split into three vertical panes corresponding to the execution stages:

1. **Input Transform** — CodeMirror JS editor
2. **SQL Queries** — one CodeMirror SQL editor per query, with a label field. Add/remove queries with the + button. Use `:variable_name` for named parameters. Queries can use `CALL procedure_name(:param)` to invoke stored procedures.
3. **Output Transform** — CodeMirror JS editor

The header bar shows the workflow name, lock status, version (read-only), and action buttons including **Test**, **History**, and **Publish**. The lock indicator shows green "Locked" while the editor holds the lock, or red "Lock lost" if the lock expired or was taken by another session.

Below the header bar is a **tab bar** with three tabs:

- **Pipeline** — the three-pane editor (Input Transform, SQL Queries, Output Transform) and the test panel.
- **Stored Procedures** — the integrated stored procedure editor (see below).
- **Tests** — test case editor for defining assertion-based test cases (see below).

### Test Execution Panel

The collapsible test panel at the bottom of the Pipeline tab has:

- **Input Parameters** — a JSON editor for test input (horizontally resizable via the divider).
- **Result tabs** — one tab per execution stage showing the output or error.
- **Rerun buttons** — each tab has a rerun icon to re-execute that single stage using the current editor content, without running the full pipeline.
- **Console tab** — a fixed tab that aggregates all `console.*` output from JS stages, color-coded by level. Rerun output is appended to the console log.

### Library Picker

The Input and Output transform editors have a **Library** button that lets you insert a canned script from the script library.

### AI Code Assist

Each code editor (Input Transform, Output Transform, SQL Queries, and Test Assertions) has an **AI** button that opens a prompt modal. The user describes what the code should do, and the AI generates a replacement using the full workflow definition and current editor contents as context.

Requires the `ANTHROPIC_API_KEY` environment variable. The AI calls the Anthropic Messages API with a system prompt tailored to each editor type (input transform, output transform, SQL, or assertion). The generated code replaces the editor contents.

See the Configuration section for the relevant environment variables.

### Stored Procedures Tab

The **Stored Procedures** tab in the workflow editor provides a sidebar + editor layout:

**Sidebar** (left) — lists all stored procedures. Click one to edit. The **+** button creates a new procedure. Each item has a delete action.

**Editor** (right) — appears when a procedure is selected. Split into two resizable panes:

1. **SQL Definition** — CodeMirror SQL editor for the `CREATE PROCEDURE` statement. Changes are auto-saved to the config database (same debounce pattern as the pipeline editor) but are **not deployed** until explicitly requested.
2. **Test** — a SQL input area and results table for running arbitrary SQL against the data database. Disabled when there are undeployed changes (the test would run against the old version).

The editor bar provides:

- **Name** — read-only, derived from the `CREATE PROCEDURE` statement in the SQL definition.
- **Description** — modal for editing the procedure description.
- **Deploy to Production** — saves and deploys the SQL to the production data database after a confirmation dialog. The procedure is live immediately.

All dividers (sidebar ↔ editor, SQL pane ↔ test pane) are resizable via drag handles.

A warning is shown when first switching to the Stored Procedures tab, reminding that deployments affect the production database immediately.

**Publishing** a workflow does **not** deploy stored procedures. Procedures must be deployed independently via the Deploy to Production button.

### Tests Tab

The **Tests** tab lets you define test cases that validate the workflow end-to-end. Each test case has:

- **Name** — descriptive label.
- **Schema Validation** — optional JSON Schema from the catalog. If set, the workflow output is validated against the schema before assertion scripts run. Manage schemas in the **JSON Schemas** tab.
- **Input** (1/3 width) — JSON input passed to the workflow pipeline.
- **Assertion script** (2/3 width) — JavaScript executed server-side via goja with `output` and `error` globals. Throw to fail.

Both panes are resizable (horizontally between panes, vertically within each pane).

**Run All Tests** sends a single `POST /api/v1/workflows/{id}/run-tests` request that executes all test cases on the server. Individual tests can be run with the per-card Run button.

**Production gate** — setting a version as production requires all test cases in that version to pass first. A modal shows individual test results and blocks promotion if any fail.

Test cases are included in version snapshots when a workflow is published.

## Seeding Example Data

The `seed-test-data` command creates example stored procedures and workflows that demonstrate how to use procedures inside workflows:

```bash
make seed-test-data
```

To re-seed after the examples have been modified, clear existing data first:

```bash
make clear-workflows    # clears workflows, versions, and stored procedures
make seed-test-data     # re-creates examples
```

After seeding, deploy the stored procedures from the admin UI (open each procedure and click **Deploy to Production**) to create them in the data database.

### Seeded Examples

| Type | Name | Description |
|------|------|-------------|
| JSON Schema | `config-summary-output` | Validates config-summary workflow output structure |
| JSON Schema | `team-details-output` | Validates team-details workflow output structure |
| JSON Schema | `dashboard-report-output` | Validates team-dashboard-report workflow output structure |
| Stored Procedure | `get_entity_counts` | Returns row counts for all config tables, filtered by minimum |
| Stored Procedure | `get_team_sources` | Returns data sources for a team |
| Stored Procedure | `get_dashboard_summary` | Returns dashboards with diagram counts for an owner |
| Workflow | `config-summary` | Calls `get_entity_counts` procedure, builds a summary object |
| Workflow | `team-details` | Mixes direct SQL (team info) with `get_team_sources` procedure call |
| Workflow | `team-dashboard-report` | Multi-step: resolves team, then calls `get_dashboard_summary` and `get_team_sources` in parallel |

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `WORKFLOW_DB_HOST` | `DB_HOST` value | Database host for SQL query execution |
| `WORKFLOW_DB_PORT` | `DB_PORT` value | Database port for SQL query execution |
| `WORKFLOW_DB_USER` | `DB_USER` value | Database user for SQL query execution |
| `WORKFLOW_DB_PASSWORD` | `DB_PASSWORD` value | Database password for SQL query execution |
| `WORKFLOW_DB_NAME` | `DB_NAME` value | Database name for SQL query execution |
| `WORKFLOW_BASE_URL` | `http://localhost:<PORT>` | Base URL shown in the editor for the live endpoint |
| `ANTHROPIC_API_KEY` | *(none)* | Anthropic API key for AI code assist (feature disabled if unset) |
| `ANTHROPIC_MODEL` | `claude-sonnet-4-20250514` | Model to use for AI code generation |
| `ANTHROPIC_API_URL` | `https://api.anthropic.com` | Base URL for the Anthropic API |

The `WORKFLOW_DB_*` variables allow workflows to query a different database (e.g., the DevLake data lake) while the config store uses the dashboard's own MySQL instance.

## VS Code Extension

The LFM Workflow Editor extension in `vscode-extension/` lets you edit workflow
JavaScript and SQL in VS Code. From the dashboard repository root, run one of:

```sh
cd vscode-extension
make build    # create devlake-workflow-editor.vsix
# or:
make install  # build and install using the VS Code `code` command
```

Alternatively, install a built VSIX from VS Code using **Extensions: Install
from VSIX…**. Configure these settings in VS Code:

| Setting | Description | Default |
|---|---|---|
| `lfm.serverUrl` | Base URL of the LFM dashboard server | `http://localhost:9090` |
| `lfm.apiKey` | API key for authentication | *(empty)* |

For a shared environment, use its dashboard API base URL and your own dashboard
API key.

Run **LFM: Open Workflow** from the Command Palette and select a workflow. It
opens that workflow in its own VS Code window. Its files typically look like:

```
workflow-name/
├── input.js                 # Input transformation
├── output.js                # Output transformation
├── test_input.json          # Input for a manual run
├── sql/
│   ├── step-0/              # Queries in this step run in parallel
│   │   └── first_query.sql
│   └── step-1/              # Runs after step-0 completes
│       └── next_query.sql
├── tests/
│   └── test-name/
│       ├── input.json       # Test input
│       └── assert.js        # Test assertions
└── docs/                    # Workflow-specific reference material
```

`input.js` and `output.js` transform workflow inputs and results. Queries in the
same `sql/step-N/` directory run in parallel; steps run sequentially. Each test
case consists of an input payload and an assertion script.

The extension supports running workflows and tests, refreshing reference docs,
publishing to staging, and reviewing version history. See the
[LFM Workflow Editor guide](../vscode-extension/README.md) for full setup and
command details.

Edits are automatically saved to the server as a draft; wait for the status
bar to show `Saved` before publishing. Opening a workflow acquires an editing
lock, and closing its VS Code window releases it. If another session holds the
lock, the extension reports that and does not open the workflow.

Publishing a draft creates a numbered staging version. Selecting a version for
production is a separate action in **LFM: Version History**. The dashboard's
environment and the workflow version selected for production are separate
settings; verify which workflow endpoint the dashboard uses when validating a
release.
