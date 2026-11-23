# DevLake Integration Guide

This document describes how the dashboard lfmbot integrates with [Apache DevLake](https://devlake.apache.org/) to manage blueprints, connections, and scopes. It is intended for integrators who configure the DevLake plugin or build automation on top of it.

## How It Works

When a team is onboarded, the lfmbot builds and maintains three layers of DevLake configuration on its behalf:

1. **Connection** — one per team per plugin (GitHub, GitLab, Jira, Codecov). A connection holds the team's API token and endpoint in DevLake. It is created during onboarding and can be updated later when the token is rotated. Connection IDs are stored in the team's entity config so the lfmbot can look them up during sync. Codecov tokens are stored only in DevLake; the dashboard stores its organization and service for scope matching.

2. **Scopes** — individual resources inside a connection (a GitHub repo, a GitLab project, a Jira board, or a Codecov repo). Scopes are resolved automatically: the lfmbot takes the repos and projects from the team's source configuration, searches DevLake's remote-scope API to find the matching scope ID, and registers them on the connection. Codecov scopes are derived from matching GitHub or GitLab team repositories.

3. **Blueprints** — collection jobs that tie scopes to a schedule. Each blueprint groups all scopes for one `{team, plugin, owner}` combination, so a team with repos from two GitHub orgs gets two GitHub blueprints. Blueprints are labeled `managed:lfmbot` and `team:{teamID}` so the lfmbot can identify which ones it owns.

The relationship is: **connection 1 → N blueprints → N scopes per blueprint**. A connection is shared across all of a team's blueprints for the same plugin, but each blueprint carries its own set of scopes.

### Sync reconciliation

Calling the sync endpoint triggers a declarative reconciliation: the lfmbot computes the desired set of blueprints from the team's current sources, compares it against the blueprints it manages (`managed:lfmbot`), and creates, updates, or deletes those blueprints to converge. Manually created Codecov blueprints attached to a team are left intact; their scopes are deduplicated against managed blueprints.

### Duplicate repos and projects

A scope can only belong to one blueprint across all teams. Before creating or updating blueprints, the lfmbot loads every existing blueprint and builds a global scope map. If a team's desired scope already appears in another team's blueprint, that scope is **skipped** — it is not added and the sync response reports it in the `skipped` array with the name of the owning blueprint. This prevents duplicate data collection in DevLake.

The first team to sync a scope owns it. To move a scope between teams, remove it from the current owner's sources and sync that team first (which deletes the blueprint), then add it to the new team and sync.

### Team deletion and automatic migration

When a team is deleted, the lfmbot runs a cleanup step before removing the team from the database:

1. Managed blueprints labeled `team:{teamID}` are deleted from DevLake, freeing their scopes. Attached manual Codecov blueprints only lose the team label; their schedule, scopes, and connection remain in DevLake.
2. The lfmbot lists all remaining teams and checks which ones have sources that use the same plugins as the freed blueprints.
3. Each affected team is re-synced. Because the deleted team's blueprints are gone, scopes that were previously blocked by dedup are now available and get picked up automatically.

This means if team A owned `org/repo` and team B also had it in its sources (but was dedup-skipped), deleting team A automatically creates a blueprint for team B. The migration is reported in the cleanup result's `migrated` array.

## Overview

The lfmbot acts as a control plane that translates team data-source configurations (git repos, Jira projects) into DevLake **blueprints**. Each blueprint tells DevLake which repositories or projects to collect data from, at what schedule, and through which connection.

```
┌──────────────────────────────────┐      ┌─────────────────────┐
│  lfmbot                          │      │  DevLake Instance   │
│                                  │      │                     │
│  Teams ─── Sources ───┐          │      │  Connections        │
│    ├── gitrepos        ├─ sync ──┼─────▶│  Scopes             │
│    └── workitems       │         │      │  Blueprints         │
│                        │         │      │                     │
│  Entity Config         │         │      │                     │
│    └── connection IDs ─┘         │      │                     │
└──────────────────────────────────┘      └─────────────────────┘
```

The integration is optional. When enabled, it provides:

- **Connection management** — create DevLake connections for GitHub, GitLab, Jira, and Codecov per team.
- **Blueprint sync** — reconcile team sources into DevLake blueprints (create, update, delete).
- **Scope deduplication** — prevent the same repository or project from appearing in multiple teams' blueprints.
- **Workflow execution** — run SQL queries against the DevLake database through the workflow engine.

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DEVLAKE_API_URL` | Yes | — | Base URL of the DevLake REST API (e.g. `http://devlake:8080/api/rest`) |
| `DEVLAKE_API_KEY` | No | — | Bearer token for DevLake API authentication |
| `DEVLAKE_CRON_CONFIG` | No | `0 0 * * *` | Base cron schedule for blueprint data collection (see [Cron Spread](#cron-spread)) |
| `DEVLAKE_SKIP_TOKEN_TEST` | No | `false` | Skip mandatory token validation during onboarding (for local dev) |

The integration is enabled when `DEVLAKE_API_URL` is set and a MySQL config store is available. Otherwise it is silently disabled.

### Cron Spread

The `DEVLAKE_CRON_CONFIG` value is not applied literally to each blueprint. To avoid all blueprints firing at the same time and overloading DevLake, the lfmbot **replaces the minute and hour fields** with values derived from a SHA-256 hash of the blueprint name. This distributes blueprint runs evenly across the day while remaining deterministic (the same blueprint always gets the same schedule).

Only the last three cron fields (day-of-month, month, day-of-week) are used as-is from the configured value:

| Config value | Effective behavior |
|--------------|--------------------|
| `0 0 * * *` (default) | Each blueprint runs once daily at a unique minute and hour |
| `0 0 * * 1-5` | Each blueprint runs once per weekday at a unique minute and hour |
| `0 0 1 * *` | Each blueprint runs on the 1st of each month at a unique minute and hour |

### Workflow Database

Workflows execute SQL against a database configured separately from the config store. This is typically the DevLake data lake:

| Variable | Fallback | Description |
|----------|----------|-------------|
| `DEVLAKE_DB_HOST` | `DB_HOST` | DevLake database host |
| `DEVLAKE_DB_PORT` | `DB_PORT` | DevLake database port |
| `DEVLAKE_DB_USER` | `DB_USER` | DevLake database user |
| `DEVLAKE_DB_PASSWORD` | `DB_PASSWORD` | DevLake database password |
| `DEVLAKE_DB_NAME` | `DB_NAME` | DevLake database name |

Set these to the DevLake MySQL instance so workflows can query collected data (pull requests, issues, commits, etc.).

## Concepts

### Connections

A DevLake **connection** represents credentials and an endpoint for a specific plugin (GitHub, GitLab, Jira, Codecov). The lfmbot creates one connection per team per plugin, named `lfmbot-{teamID}-{plugin}`.

Connection IDs are stored in the team's entity config under the key `devlake_{plugin}_connection_id` (e.g. `devlake_github_connection_id`).

### Scopes

A **scope** is a specific resource within a connection — a GitHub repo, a GitLab project, a Jira board, or a Codecov repo. GitHub, GitLab, and Jira use numeric IDs (`githubId`, `gitlabId`, `boardId`); Codecov uses a string `codecovId` such as `konflux-ci/konflux-ui`.

The lfmbot resolves scopes by searching DevLake's remote scope API using the owner/name pattern from the team's source configuration.

### Codecov setup in the dashboard

For an existing team, add its GitHub or GitLab repository under **My Teams → Sources**. Under **Token Management**, add a **Team / DevLake** connection and choose **Codecov**. Enter the Codecov API endpoint (default `https://api.codecov.io`), organization owner (for example `konflux-ci`), repository service, token, and expiry. Test and save the connection. The organization must match the owner of at least one team repository on the selected service. GitHub and GitLab, including their enterprise service variants, are supported; Bitbucket team repositories are not currently managed by the dashboard.

For a new team, select Codecov in the **Connections** step and add a matching GitHub or GitLab repository in **Sources**. A separate GitHub or GitLab DevLake connection is optional when you only need Codecov: enter the repository manually, using the Codecov service and organization. Without a Git provider token, the wizard cannot browse or pre-validate that repository; the onboarding sync resolves it through Codecov and reports a scope error if it cannot be found. The onboarding sync creates the Codecov blueprint automatically. For an existing team, use **Admin → Teams → Sync DevLake Blueprints** after saving the connection. Then use **Trigger DevLake Run** to collect data. The Codecov blueprint is labeled `team:{teamID}` and contains the matching repository scopes. Check the DevLake pipeline result before comparing dashboard metrics.

If the DevLake instance already has a working Codecov connection and blueprint, an admin can use **Token Management → Attach Existing Codecov** instead of creating a new token. Select the team, Codecov connection, and blueprint. The blueprint's Codecov scopes must match GitHub or GitLab repositories already configured for that team and cannot already belong to another blueprint; attaching adds only the team label and stores the connection metadata in dashboard config. It does not duplicate the connection, change the blueprint schedule, or copy the token into the dashboard. **Trigger DevLake Run** then includes that blueprint. Sync does not replace or delete it, and deleting the team detaches it without deleting the DevLake resources.

The dashboard stores the Codecov connection ID, endpoint, organization, service, and token expiry in team config. It does not store the Codecov token; DevLake owns that credential. Codecov coverage charts still require migrated production workflows and collected coverage data.

### TestRegistry setup in the dashboard

The DevLake `testregistry` plugin (Konflux fork) collects CI test results: jobs, JUnit test suites and test cases. Under **Token Management**, add a **Team / DevLake** connection, choose **TestRegistry**, then pick a **CI Tool**:

| CI Tool | Organization | Token | Scopes offered | Collected data |
|---------|--------------|-------|----------------|----------------|
| `Openshift CI` | GitHub organization | GitHub token (required, used to list configs) | Repos under `openshift/release/ci-operator/config/<org>`; the scope full name is the bare repo name | Prow jobs from `prow.ci.openshift.org/prowjobs.js`, JUnit artifacts from GCS |
| `Tekton CI` | Quay.io organization | none | Repositories in the Quay namespace; the scope full name is `<quay-org>/<repo>` (the repo part may contain `/`) | Tekton PipelineRun artifacts pulled as OCI images (tags in the sync window, default 6 months) |

Both collectors write to the same tables (`ci_test_jobs`, test suites, test cases), and the rows do not record which CI tool produced them. Each team has one TestRegistry connection, so it uses one CI tool. To collect from both Prow and Tekton, use two teams.

The **CI tool and organization cannot be changed** after the connection is created: the UI shows them read-only and the API rejects changes with `400`. Both collectors run on every pipeline, and each returns early when the connection's CI tool doesn't match. So scopes registered from the other source would quietly collect nothing. An Openshift CI connection can still rotate its GitHub token; a Tekton CI connection has nothing to update.

DevLake's plugin marks the connection's `project` field as required, but nothing in the plugin reads it. The dashboard uses blueprints, not DevLake projects, and always sends the placeholder `lfmbot-placeholder`. This workaround can go once the plugin makes the field optional.

Scopes are added under **My Teams → Sources → Test Suites (TestRegistry)**. You can browse them from the connection's `remote-scopes` (the plugin has no search endpoint) or enter them by full name. They are stored in a `testsuites` source as `workitem_projects` rows with `type = "testregistry"` and the scope full name in `project_name`. That keeps them out of the `repos` table that the code dashboards use. Onboarding checks every name against the connection's remote scopes and saves its canonical spelling. Sync builds the scope payloads directly from the full name (the scope ID) and manages one blueprint per team, `lfmbot-{teamID}-testregistry-{organization}`. Deduplication keys testregistry scopes by CI tool plus organization, because the connections have no endpoint.

The **New Team** wizard supports TestRegistry in its **Connections** step (CI tool, organization, and a GitHub token for Openshift CI only) and its **Sources** step. Because the DevLake connection doesn't exist yet, the wizard browses scopes through `POST /api/v1/devlake/proxy-browse` with `"plugin": "testregistry"`. The sidecar then reads the same provider data the plugin uses: the GitHub contents API for `openshift/release/ci-operator/config/<org>` (trying `master`, then `main`), or the Quay.io repository API (`public=true`, paginated). Scope names typed in by hand are checked against that list before the team is created. Team import does not support TestRegistry yet.

### Blueprints

A **blueprint** is a DevLake collection job that defines which scopes to sync and on what schedule. The lfmbot creates blueprints named `lfmbot-{teamID}-{plugin}-{owner}` and labels them:

- `managed:lfmbot` — marks blueprints as lfmbot-managed
- `team:{teamID}` — associates the blueprint with a team

Only blueprints with these labels are modified during sync. Manually created blueprints in DevLake are left untouched.

## REST API

Onboarding and team connection management require authentication; connection changes are limited to the team's contact or an admin. Blueprint sync and trigger endpoints require admin authentication.

### Onboard a team

```
POST /api/v1/onboard
```

**Authentication:** any authenticated user (not admin-only).

Atomic endpoint that creates a team with all its connections, sources, dashboards, and config in a single request. Used by the onboarding wizard. The handler validates consistency before creating anything — for example, sources that reference a plugin (e.g. `github` repos) are rejected unless a matching connection is provided.

Request:
```json
{
  "team": {"id": "my-team", "name": "My Team", "contact_email": "team@example.com"},
  "product_id": "my-product",
  "connections": [
    {"plugin": "github", "token": "ghp_...", "endpoint": "https://api.github.com/", "expiry_date": "2027-03-15"}
  ],
  "sources": [
    {"id": "repos", "type": "gitrepos", "repos": [
      {"id": "r1", "type": "github", "owner": "myorg", "name": "myrepo"}
    ]}
  ],
  "dashboards": [
    {"dashboard_id": "d1", "type": "grafana", "title": "Main Dashboard"}
  ],
  "config": {"key1": "value1"},
  "jira_scope_configs": {
    "PROJ": {
      "requirement_types": ["Story", "Task"],
      "bug_types": ["Bug"],
      "story_point_field": "customfield_10016"
    }
  }
}
```

Validation rules:
- `team.id` and `team.name` are required.
- `product_id` is optional; when provided, the team is linked to the product via the `product_teams` join table.
- Each connection requires `plugin`, `token`, `endpoint`, and a valid `expiry_date` (`YYYY-MM-DD` or `never`).
- Jira connections require `username`.
- Codecov connections require `organization` and accept `service` (`github` by default).
- Git repo sources referencing a plugin type must have a matching connection.
- Work item sources referencing Jira must have a Jira connection.
- `jira_scope_configs` is optional; maps Jira project keys to DevLake scope config (issue type mappings and story point field).

The handler creates the team, connections, config, sources, and dashboards in order, then triggers a DevLake sync if sources are present. The response includes the sync result when applicable.

Response `201`:
```json
{
  "team_id": "my-team",
  "status": "created",
  "sync": {
    "created": ["lfmbot-my-team-github-myorg"],
    "updated": [],
    "deleted": [],
    "skipped": [],
    "errors": []
  }
}
```

### Offboard a team

```
POST /api/v1/offboard
```

**Authentication:** team manager or admin.

Deletes a team and all its associated resources (sources, dashboards, config, connections). If DevLake integration is enabled, DevLake blueprints are cleaned up first and scopes are migrated to other teams that reference the same repos.

Request:
```json
{"team_id": "my-team"}
```

Response `200`:
```json
{
  "team_id": "my-team",
  "status": "deleted",
  "cleanup": {
    "deleted_blueprints": ["lfmbot-my-team-github-myorg"],
    "migrated": ["lfmbot-other-team-github-myorg"],
    "errors": []
  }
}
```

### Add sources to an existing team

```
POST /api/v1/onboard-source
```

**Authentication:** team manager or admin.

Adds repos or projects to an existing team's source configuration and triggers a DevLake sync. Used by the team settings UI to add sources after initial onboarding.

Request:
```json
{
  "team_id": "my-team",
  "source_id": "repos",
  "items": [
    {"id": "r2", "type": "github", "owner": "myorg", "name": "new-repo"}
  ]
}
```

TestRegistry scopes go to the `testsuites` source as `{"type": "testregistry", "project_name": "<scope full name>"}` items. Each name must be offered by the team's TestRegistry connection; unknown names are rejected with `400`, and names that are already configured are skipped with a validation warning.

### Remove a source from an existing team

```
POST /api/v1/offboard-source
```

**Authentication:** team manager or admin.

Removes a single repo or project from a team's source configuration and triggers a DevLake sync to clean up the blueprint.

Request:
```json
{
  "team_id": "my-team",
  "source_id": "repos",
  "item_id": "r2"
}
```

### Sync blueprints

```
POST /api/v1/devlake/teams/{teamId}/sync
```

Triggers a full reconciliation of the team's data sources against DevLake. The sync pipeline:

1. **Build desired state** — reads team sources from the config store, maps repos and work-item projects to DevLake plugins, looks up connection IDs from entity config.
2. **Resolve scopes** — searches DevLake's remote scope API to find the numeric scope ID for each repo/project.
3. **Deduplicate** — checks all existing blueprints; skips any scope already claimed by another blueprint. Scopes are keyed by provider endpoint + scope ID, so a repo/project/board owned by another team is skipped even when the teams use separate connections to the same provider instance (two distinct provider instances never collide). Endpoints are canonicalized to the provider host and port — scheme, userinfo, any path behind the base address (e.g. GitLab's `/api/v4`), query strings, and fragments are ignored — so connections that differ only in format key identically.
4. **Register scopes** — PUTs scope data to DevLake to ensure scopes that survived dedup are registered before creating blueprints.
5. **Reconcile** — compares desired blueprints against existing ones labeled `team:{teamID}`:
   - Creates new blueprints for new sources.
   - Updates blueprints whose scopes have changed.
   - Deletes blueprints whose sources were removed.
   - Leaves unchanged blueprints untouched.

Response `200`:
```json
{
  "created": ["lfmbot-myteam-github-myorg"],
  "updated": [],
  "deleted": ["lfmbot-myteam-github-oldorg"],
  "skipped": ["myorg/shared-repo already collected by team otherteam (blueprint \"lfmbot-otherteam-github-myorg\"); existing data will be used for this team's metrics"],
  "errors":  []
}
```

Each field is an array of strings. `skipped` lists scopes that were deduplicated. `errors` lists non-fatal issues (missing connections, scope resolution failures) that did not prevent the sync from completing.

### Create a connection

```
POST /api/v1/devlake/teams/{teamId}/connections
```

Creates a single DevLake connection for a team. The connection is named `lfmbot-{teamID}-{plugin}` and the resulting connection ID and token expiry date are stored in the team's entity config. Codecov tokens remain in DevLake.

Request:
```json
{
  "plugin": "github",
  "token": "ghp_...",
  "endpoint": "https://api.github.com/",
  "expiry_date": "2027-03-15"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `plugin` | Yes | DevLake plugin name: `github`, `gitlab`, `jira`, `codecov`, or `testregistry` |
| `token` | Yes, except TestRegistry | Access token for the selected provider; for TestRegistry only `Openshift CI` needs one (a GitHub token) |
| `endpoint` | Yes, except TestRegistry | API base URL for the service |
| `expiry_date` | Yes | Token expiry date in `YYYY-MM-DD` format or `never`; defaults to `never` for a TestRegistry connection without a token |
| `username` | Jira only | Email address for Jira basic auth |
| `organization` | Codecov, TestRegistry | Codecov organization owner matching the team's GitHub or GitLab repository owner; for TestRegistry, the GitHub (`Openshift CI`) or Quay.io (`Tekton CI`) organization |
| `service` | Codecov only | `github` (default), `gitlab`, `github_enterprise`, or `gitlab_enterprise` |
| `ci_tool` | TestRegistry only | `Openshift CI` or `Tekton CI`; cannot be changed once the team has a connection |

Plugin-specific behavior:

| Plugin | Auth Method | Extra Config |
|--------|-------------|--------------|
| `github` | `AccessToken` | `enableGraphql: true` |
| `gitlab` | (default) | — |
| `jira` | `BasicAuth` | `password` = token, `username` from request |
| `codecov` | Bearer token | `organization` and `service` from request |
| `testregistry` | `githubToken` (`Openshift CI` only) | `ciTool`, `githubOrganization` or `quayOrganization`, placeholder `project`; no endpoint |

Response `201`:
```json
{
  "plugin": "github",
  "connection_id": 1
}
```

### Attach an existing Codecov blueprint

```
GET  /api/v1/devlake/teams/{teamId}/codecov/available
POST /api/v1/devlake/teams/{teamId}/codecov/attach
```

Both endpoints require an admin. `available` returns Codecov connection metadata (never tokens) and unattached, non-managed Codecov blueprints with their scope IDs. Attach with `{"connection_id":1,"blueprint_id":1}`. The selected blueprint must use only that Codecov connection, and every scope must match a GitHub or GitLab repository in the team without already belonging to another blueprint. The dashboard adds `team:{teamID}` to the blueprint labels and saves the connection ID/metadata, without changing the collection schedule or credentials.

### Update a connection

```
PUT /api/v1/devlake/teams/{teamId}/connections/{plugin}
```

Updates the token and expiry date for an existing connection. The new token is sent to DevLake via PATCH, and the expiry date is updated in the team's entity config.

Request:
```json
{
  "token": "ghp_new_token...",
  "expiry_date": "2027-06-30",
  "endpoint": "https://api.github.com/"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `token` | Yes | New access token |
| `expiry_date` | Yes | New expiry date in `YYYY-MM-DD` format |
| `endpoint` | No | Updated endpoint URL (if omitted, the existing endpoint is preserved) |
| `username` | Jira only | Updated email for Jira basic auth |
| `organization` | Codecov only | Updated organization owner |
| `service` | Codecov only | Updated repository service |

For `testregistry`, only an `Openshift CI` connection's GitHub token can be updated. Sending a different `ci_tool` or `organization` returns `400`.

Response `200`:
```json
{"status": "updated"}
```

### Get token status

```
GET /api/v1/devlake/teams/{teamId}/token-status
```

Returns the expiry status of all configured connections for a team. Used by the UI to show expiry badges and warn about approaching expiration.

Response `200`:
```json
[
  {
    "plugin": "github",
    "connection_id": 1,
    "expiry_date": "2027-03-15"
  },
  {
    "plugin": "jira",
    "connection_id": 2,
    "expiry_date": "2026-12-01"
  }
]
```

### List existing scopes

```
GET /api/v1/devlake/teams/{teamId}/existing-scopes/{plugin}
```

Lists all scopes across all blueprints for a given plugin, excluding scopes owned by the specified team. Used by the admin UI to show which repos are already tracked by other teams.

| Parameter | Location | Description |
|-----------|----------|-------------|
| `teamId` | Path | Team whose blueprints are excluded from results |
| `plugin` | Path | Plugin name to filter by (`github`, `gitlab`, `jira`, `codecov`) |

Response `200`:
```json
[
  {
    "full_name": "myorg/myrepo",
    "connection_name": "lfmbot-otherteam-github",
    "connection_id": 1
  }
]
```

### Test a connection

```
POST /api/v1/devlake/test-connection
```

Tests whether the provided credentials can connect to the target system. Codecov and GitHub use DevLake's `/plugins/{plugin}/test` endpoint so that the dashboard and DevLake always agree on whether a token is usable; GitLab, Jira, and TestRegistry use provider API checks (TestRegistry runs the plugin's own GitHub/Quay.io checks directly, so testing works without DevLake being reachable). Token Management requires a passing test before saving a connection.

For GitHub, DevLake's test also verifies token scopes: a classic PAT missing the scopes DevLake needs to collect issues, PRs, and accounts (at minimum `repo`, `read:user` and `read:org`) fails the test with a message listing the required permissions instead of letting onboarding complete with a token that later fails during sync.

For GitLab, the token's scopes are read from `GET /api/v4/personal_access_tokens/self` and the token must carry the `read_api` (or `api`) scope — a repository-only token authenticates but cannot list projects, which the repository browser and DevLake collection both require. On GitLab instances where the self endpoint is unavailable, the test falls back to `/api/v4/user` and cannot verify scopes.

Request:
```json
{
  "plugin": "github",
  "token": "ghp_...",
  "endpoint": "https://api.github.com/"
}
```

Response `200`:
```json
{"success": true, "message": "Connection successful"}
```

On failure:
```json
{"success": false, "message": "test github connection: The provided token is missing one or more of the following permissions: repo, read:user and read:org"}
```

For Codecov, include `"organization": "konflux-ci"` and `"service": "github"` in the request.

For TestRegistry, send `"plugin": "testregistry"`, `"ci_tool"` and `"organization"` (plus `"token"` for `Openshift CI`), with no endpoint. The sidecar runs the same checks as the plugin directly, without DevLake: for `Openshift CI` it checks the token (`GET /user`, which also fills in the token expiry) and access to the organization (`GET /orgs/<org>`) on GitHub; for `Tekton CI` it checks that the Quay.io namespace can be listed.

### Browse provider repos/projects

```
POST /api/v1/devlake/proxy-browse
```

Server-side proxy that browses GitHub/GitLab repos or Jira projects directly from the provider API. Used by the onboarding wizard to let users pick repositories without exposing tokens to the browser (avoids CORS issues with self-hosted instances).

Request:
```json
{
  "plugin": "github",
  "endpoint": "https://api.github.com",
  "token": "ghp_..."
}
```

For Jira, include `"username": "user@example.com"`.

Response `200`:
```json
[
  {"key": "myorg/myrepo", "name": "myrepo"},
  {"key": "myorg/other-repo", "name": "other-repo"}
]
```

### Fetch Jira project metadata

```
POST /api/v1/devlake/proxy-jira-meta
```

Fetches issue types and custom fields for a Jira project. Used by the wizard's Jira Settings step to populate requirement type, bug type, and story point field selections.

Request:
```json
{
  "plugin": "jira",
  "endpoint": "https://jira.example.com",
  "token": "jira_token",
  "username": "user@example.com",
  "project_key": "PROJ"
}
```

Response `200`:
```json
{
  "issue_types": [{"name": "Story"}, {"name": "Bug"}, {"name": "Task"}],
  "fields": [{"id": "customfield_10016", "name": "Story Points", "custom": true}]
}
```

### Source type restrictions

The onboarding wizard only allows adding data sources for plugins that have a connection configured in the connections step. For example, if only a GitHub connection is provided, the user can add GitHub repositories but not GitLab repositories or Jira projects. The repo type dropdown and "Add Work Items" button are filtered accordingly.

## Sync Lifecycle

The onboarding wizard uses the single `POST /api/v1/onboard` endpoint, which handles all steps atomically. For reference, the equivalent sequence using individual admin endpoints:

```
1. Onboard (wizard flow — single atomic call)
   POST /api/v1/onboard
   {
     "team": {"id": "my-team", "name": "My Team"},
     "connections": [{"plugin": "github", "token": "ghp_...", "endpoint": "https://api.github.com/", "expiry_date": "2027-03-15"}],
     "sources": [{"id": "repos", "type": "gitrepos", "repos": [{"id": "r1", "type": "github", "owner": "myorg", "name": "myrepo"}]}],
     "dashboards": [{"dashboard_id": "d1", "type": "grafana", "title": "Main"}],
     "config": {}
   }
   → Creates team, connections, sources, dashboards, config, and triggers sync

2. DevLake runs the blueprint on its spread cron schedule
   → Data flows into the DevLake database

3. Query collected data via workflows
   GET /workflows/production/team-details?team_id=my-team
   → Executes SQL against the DevLake database, returns structured results
```

## Scope Deduplication

The dedup step prevents the same scope from being collected by multiple blueprints, which would cause duplicate data in DevLake. During sync:

1. All existing blueprints are loaded (all teams, all plugins).
2. Blueprints belonging to the current team (`team:{teamID}` label) are excluded.
3. A global scope map is built from remaining blueprints.
4. Any scope in the desired state that already exists in the global map is moved to `skipped`.

This means the **first team to sync a scope owns it**. If a repo needs to move between teams, remove it from the current team's sources and sync that team first, then add it to the new team.

## Token Expiry Tracking

Every DevLake connection requires a token expiry date (`YYYY-MM-DD`). This is mandatory in both the onboarding wizard and the team settings UI, and enforced by the API (`POST` and `PUT` on connections return `400` without it).

Expiry dates are stored in the team's entity config as `devlake_{plugin}_token_expiry` (e.g. `devlake_github_token_expiry`).

### UI indicators

The team settings form shows a color-coded badge next to each connection:

| Badge | Condition |
|-------|-----------|
| Green ("Expires YYYY-MM-DD") | More than 14 days until expiry |
| Yellow ("Expires YYYY-MM-DD") | 14 days or fewer until expiry |
| Red ("Expires YYYY-MM-DD") | Token has expired |
| Grey ("No expiry date set") | Expiry date not recorded (legacy connections) |

### Renewing a token

Users can update a connection's token and expiry date from the team settings form ("Update Token" button) or via the API:

```
PUT /api/v1/devlake/teams/{teamId}/connections/{plugin}
{"token": "new_token", "expiry_date": "2027-06-30"}
```

This patches the token on the DevLake connection and updates the expiry date in entity config.

## Error Handling

The sync is designed to be **partial-success tolerant**:

- Individual scope resolution failures are recorded in `errors` but do not abort the sync.
- Missing DevLake connections produce an error message but other plugins continue.
- Blueprint create/update/delete failures are recorded but do not prevent other blueprints from being processed.

Callers should check the `errors` array even on HTTP 200 — a successful status means the sync pipeline completed, not that every operation succeeded.

## Blueprint Naming and Labels

| Convention | Pattern | Example |
|------------|---------|---------|
| Blueprint name | `lfmbot-{teamID}-{plugin}-{owner}` | `lfmbot-platform-github-myorg` |
| Connection name | `lfmbot-{teamID}-{plugin}` | `lfmbot-platform-github` |
| Managed label | `managed:lfmbot` | — |
| Team label | `team:{teamID}` | `team:platform` |

The lfmbot only manages blueprints that carry both the `managed:lfmbot` and `team:{teamID}` labels. Blueprints created manually in DevLake or by other tools are never modified.

## DevLake API Endpoints Used

The lfmbot interacts with the following DevLake REST API endpoints:

| Operation | Method | DevLake Endpoint |
|-----------|--------|------------------|
| List blueprints | GET | `/blueprints?pageSize=100&page={n}&label={label}` |
| Create blueprint | POST | `/blueprints` |
| Update blueprint | PATCH | `/blueprints/{id}` |
| Delete blueprint | DELETE | `/blueprints/{id}` |
| Search remote scopes | GET | `/plugins/{plugin}/connections/{connID}/search-remote-scopes?search={term}` |
| Register scopes | PUT | `/plugins/{plugin}/connections/{connID}/scopes` |
| Create connection | POST | `/plugins/{plugin}/connections` |
| Update connection | PATCH | `/plugins/{plugin}/connections/{connID}` |
| List connections | GET | `/plugins/{plugin}/connections` |
| List connection scopes | GET | `/plugins/{plugin}/connections/{connID}/scopes/?pageSize=100&page={n}` |
| Test connection | POST | `/plugins/{plugin}/test` |

All requests use `Authorization: Bearer {DEVLAKE_API_KEY}`.
