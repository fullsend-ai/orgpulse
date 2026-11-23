# Importing Teams

Import legacy team JSON files into the dashboard. The import creates the team,
sets up source connections (GitHub/GitLab/Jira tokens), registers repositories,
and triggers a DevLake sync — the same steps as the onboarding wizard.

Legacy fields that no longer apply (`fetchurl`, `fetchcheckurl`, `blueprintid`,
`dashboards`) are ignored during import.

## Prerequisites

- A running dashboard instance with the admin API enabled
- An admin API key (set via `DASHBOARD_ADMIN_API_KEY` or created in the admin UI)
- Access tokens for each source type used by the team (GitHub PAT, GitLab token,
  Jira API token)

## Admin UI

1. Open the admin panel and navigate to the **Teams** tab.
2. Click **Import JSON** in the toolbar.
3. Select a legacy team JSON file. The file is validated and a preview of all
   actions is displayed.
4. Review the preview:
   - **Actions** — what will be created (team, sources, repos, connections, sync).
   - **Duplicate Repositories** — repos that already exist in other teams
     (they will still be imported).
   - **Warnings** — ignored legacy fields and other non-blocking issues.
   - **Errors** — blocking issues that prevent import.
5. Select the **Product** to assign the team to from the dropdown (optional but
   recommended — this links the team to a product so it appears in the
   dashboard).
6. Fill in the **Source Connections** section with access tokens for each
   required plugin. Each connection needs:
   - **Token** — the personal access token or API key.
   - **Endpoint** — the API endpoint URL (pre-filled with defaults for
     GitHub/GitLab).
   - **Token Expiry** — select a date or check **No expiry**.
   - **Username** — required for Jira connections only.
7. Click **Confirm Import**.
8. Review the import report showing success/error status for each action and
   the DevLake sync result.
9. Click **Download Report** to save the report JSON for potential revert.

## CLI

The `import-team` command calls the dashboard REST API to import a single team
JSON file.

### Usage

```sh
import-team [options] <team.json>
```

### Options

| Flag | Env var | Description |
|---|---|---|
| `--url` | `DASHBOARD_URL` | Dashboard base URL (default: `http://localhost:9090`) |
| `--token` | `DASHBOARD_ADMIN_TOKEN` | Dashboard admin API key (**required**) |
| `--product-id` | `DASHBOARD_PRODUCT_ID` | Product ID to link the imported team to |
| `--yes` | | Skip the confirmation prompt |
| `--report` | | Path for the report JSON (default: auto-generated) |
| `--github-token` | `GITHUB_TOKEN` | GitHub personal access token |
| `--github-endpoint` | | GitHub API endpoint (default: `https://api.github.com`) |
| `--github-expiry` | | Token expiry date (`YYYY-MM-DD` or `never`, default: `never`) |
| `--gitlab-token` | `GITLAB_TOKEN` | GitLab personal access token |
| `--gitlab-endpoint` | | GitLab API endpoint (default: `https://gitlab.com`) |
| `--gitlab-expiry` | | Token expiry date (`YYYY-MM-DD` or `never`, default: `never`) |
| `--jira-token` | `JIRA_TOKEN` | Jira API token |
| `--jira-endpoint` | | Jira API endpoint (e.g. `https://jira.example.com`) |
| `--jira-username` | `JIRA_USERNAME` | Jira username / email (required for Jira) |
| `--jira-expiry` | | Token expiry date (`YYYY-MM-DD` or `never`, default: `never`) |

### Examples

Import a team with a GitHub token, linking it to a product:

```sh
import-team \
  --url https://dashboard.example.com \
  --token my-admin-key \
  --product-id my-product \
  --github-token ghp_xxxxxxxxxxxx \
  team.json
```

Import with GitHub and Jira, skip confirmation, save report to a specific path:

```sh
import-team \
  --url https://dashboard.example.com \
  --token my-admin-key \
  --github-token ghp_xxxxxxxxxxxx \
  --jira-token jira-api-token \
  --jira-endpoint https://jira.example.com \
  --jira-username user@example.com \
  --yes \
  --report /tmp/import-report.json \
  team.json
```

Using environment variables:

```sh
export DASHBOARD_URL=https://dashboard.example.com
export DASHBOARD_ADMIN_TOKEN=my-admin-key
export GITHUB_TOKEN=ghp_xxxxxxxxxxxx

import-team team.json
```

### Process

1. The CLI reads the JSON file and sends it to the dashboard preview endpoint.
2. A preview is printed showing all planned actions, warnings, and any
   duplicate repositories.
3. The user confirms (or `--yes` skips this step).
4. The CLI sends the team data along with the provided connection tokens to the
   execute endpoint.
5. The dashboard creates the team, connections, sources, and runs a DevLake
   sync.
6. A report is printed and saved as a JSON file.

### Report file

The report JSON contains:

- `team_id`, `team_name`, `timestamp` — what was imported and when.
- `actions` — each step with its `type`, `description`, and `status`
  (`success` or `error`).
- `duplicates` — repos that also exist in other teams.
- `sync_result` — DevLake sync details (created/updated/skipped/errors).
- `revert_info` — IDs of created resources for potential manual revert.

## Legacy JSON format

The import accepts the legacy team JSON format with camelCase field names.
Example structure:

```json
{
  "id": "my-team",
  "name": "My Team",
  "contactemail": "team@example.com",
  "sources": [
    {
      "id": "gitrepos",
      "type": "gitrepos",
      "config": { "sprintstart": "2026-01-01", "sprintduration": 3 },
      "repos": [
        { "id": "my-repo", "type": "github", "owner": "org", "name": "repo" }
      ]
    },
    {
      "id": "workitems",
      "type": "workitems",
      "projects": [
        {
          "id": "jira-proj",
          "type": "jira",
          "projectname": "My Project",
          "jiraproject": "MYPROJ",
          "boardid": "123",
          "scope_config": {
            "requirement_types": ["Story", "Task"],
            "bug_types": ["Bug"],
            "story_point_field": "customfield_10016"
          }
        }
      ]
    }
  ]
}
```

Fields that are parsed but **ignored** during import:

- `fetchurl`, `fetchcheckurl` — legacy trigger/polling URLs
- `blueprintid`, `blueprintids` — DevLake blueprint IDs from the source system
- `dashboards` — will be added in a future version

A demo import JSON with Jira scope config is at `examples/demo-import.json`.

## Jira Scope Configuration

Each Jira board project in the import JSON can include an optional `scope_config`
object that tells DevLake how to classify Jira issues:

| Field | Type | Description |
|---|---|---|
| `requirement_types` | `string[]` | Issue type names mapped to REQUIREMENT (e.g. `["Story", "Task"]`) |
| `bug_types` | `string[]` | Issue type names mapped to BUG (e.g. `["Bug"]`) |
| `story_point_field` | `string` | Jira field ID for story points (e.g. `customfield_10016`) |

The `boardid` field is required when using `scope_config` — it identifies the Jira
Agile board whose DevLake scope receives the configuration.

If `scope_config` is omitted, DevLake creates a bare-bones default configuration
with `TICKET` entity tracking only (no issue type mappings or story point field).

The JSON schema for the import format is at `schemas/teams.schema.json`.
