# LFM Workflow Editor

VS Code extension for editing, testing, and running LFM dashboard
data-transformation workflows (JS + SQL).

Each workflow is opened as an isolated project in its own VS Code window.
Files auto-save to the server as you edit, with lock management to prevent
concurrent edits.

## Setup

1. Install the extension (or load it from source with `npm run compile`).
2. Run **LFM: Login** from the command palette. It asks for the server URL if
   none is configured, opens the dashboard in your browser, and asks you to
   confirm the login there. The extension then stores the generated API key in
   VS Code's secure storage (SecretStorage, backed by the OS keychain). Reload
   the window when prompted.

| Setting | Description | Default |
|---|---|---|
| `lfm.serverUrl` | Base URL of the LFM dashboard server | `http://localhost:9090` |
| `lfm.apiKey` | Fallback API key, used only when secure storage holds no key | *(empty)* |

### API key storage

The extension uses the API key from secure storage if one is stored there, and
otherwise the `lfm.apiKey` setting. The setting stays supported for manual or
scripted configuration, but its value is stored in plaintext in
`settings.json` and Settings Sync may copy it to other machines.

If the setting holds a key and secure storage is empty, the extension offers
to move the key to secure storage. Only if you accept does it store the key
securely and remove the setting from the user, workspace, and folder settings
where it is set. If secure storage is unavailable (for example, no OS keyring
on Linux), **LFM: Login** saves the key to the `lfm.apiKey` setting instead.

**LFM: Clear Stored API Key** removes the key from secure storage. It doesn't
change the `lfm.apiKey` setting; if the setting holds a key, the extension uses
it after the next reload.

### Build and install from the repository

From the dashboard repository root, run:

```sh
cd vscode-extension
make build
# or, build and install in one step:
make install
```

`make build` creates `devlake-workflow-editor.vsix`. `make install` also builds
the extension, then installs the VSIX using the VS Code `code` command. The
`code` command must be available in your shell. To install a VSIX manually, run
**Extensions: Install from VSIX…** from the VS Code Command Palette and select
the package.

## Opening a Workflow

Open the command palette (`Ctrl+Shift+P` / `Cmd+Shift+P`) and run:

- **LFM: Open Workflow** — pick from a list of existing workflows
- **LFM: Create Workflow** — create a new workflow and open it

The selected workflow downloads to a temporary directory and opens in a new
VS Code window. The file explorer shows the workflow structure directly:

If VS Code opens the workflow in **Restricted Mode**, use **Manage Workspace
Trust** and trust that workflow folder. The extension needs the folder to be
trusted to activate and load the workflow files.

```
input.js              # Input transformation (ES5.1)
output.js             # Output transformation (ES5.1)
test_input.json       # Default test input for manual runs
sql/
  step-0/             # First query stage (parallel)
    query_name.sql
  step-1/             # Second stage (runs after step-0)
    query_name.sql
tests/
  test-name/
    input.json        # Test input payload
    assert.js         # Assertion script
docs/                 # Reference documentation
  WORKFLOW_GUIDE.md
  devlake-db-schema.md
  schemas/
  stored-procedures/
  queries/
  scripts/
```

## Workflow Actions

Once a workflow is open, actions are available in three ways:

### Editor Title Bar

Icons appear in the top-right corner of the editor:

| Icon | Action |
|---|---|
| $(play) | Run Workflow |
| $(beaker) | Run Tests |
| $(cloud-upload) | Publish to Staging |
| $(history) | Version History |
| $(sync) | Refresh Reference Docs |

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl+Shift+R` (`Cmd+Shift+R` on Mac) | Run Workflow |
| `Ctrl+Shift+T` (`Cmd+Shift+T` on Mac) | Run Tests |

### Command Palette

All commands are available via `Ctrl+Shift+P` / `Cmd+Shift+P`:

| Command | Description |
|---|---|
| LFM: Run Workflow | Execute the workflow with `test_input.json` and show results |
| LFM: Run Tests | Run all test cases and show pass/fail in the output panel |
| LFM: Add Test | Create a new test case (input.json + assert.js) |
| LFM: Delete Test | Pick and delete an existing test case |
| LFM: Publish to Staging | Publish the current draft as a new staging version |
| LFM: Version History | Browse and revert to previous workflow versions |
| LFM: Refresh Reference Docs | Re-fetch schemas, procedures, queries, and DB schema |

## DevLake SQL

Admins can run ad-hoc SQL against the DevLake database, the same feature as
the **DevLake DB** tab in the admin UI. These commands work in any window, with
or without an open workflow:

| Command | Description |
|---|---|
| LFM: Run SQL on DevLake | Run the selection, or the whole active editor, and show the result table |
| LFM: New DevLake SQL Query | Open a scratch SQL editor |
| LFM: Browse DevLake Tables | Pick a table and preview its first 100 rows |

In SQL editors, run the query with the **Run on DevLake** link above the
first line, `Ctrl+Alt+D` (`Ctrl+Option+D` on Mac), or the $(database) icon in
the editor title bar. Other SQL extensions, such as Database Client, add their
own "Run" buttons and shortcuts to SQL editors; those run against their own
connections, not DevLake. Statements that don't start
with `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN`, or `WITH` ask for confirmation
first. The result panel renders up to 1000 rows; use **Open as JSON** or
**Open as CSV** for the full result.

The commands call `POST /api/v1/devlake-db/query` and
`GET /api/v1/devlake-db/tables`, which require an admin API key. Keys created
by **LFM: Login** are admin keys. The server only exposes these endpoints when
it is connected to the DevLake database; otherwise they return HTTP 404.
Queries time out after 30 seconds.

## Status Bar

The bottom-left status bar shows two indicators:

- **Workflow name** — identifies which workflow is open
- **Sync state** — `Saved`, `Saving...`, or `Save Error` (click to open the
  output panel for details)

## Auto-Save

Every file change is automatically saved to the server as a draft after a
short debounce (1 second for edits, 500ms for structural changes like
adding/removing tests). No manual save action is needed.

New files created in `sql/step-N/` are automatically registered as queries.
New directories in `tests/` are automatically registered as test cases.

## Reviewing Changes Before Publishing

The **Source Control** view (`Ctrl+Shift+G`) compares your working copy with
the current staging version (the latest published version), like git compares
local changes with upstream:

- **Changes vs staging vN** lists every workflow file you added, modified, or
  deleted since that version. The count appears on the Source Control icon.
- Click a file to open a side-by-side diff (staging on the left, your working
  copy on the right). Deleted files open the staging content.
- The editor gutter marks added, changed, and removed lines in open files, and
  the explorer shows `M`/`A`/`D` badges.
- Type a publish note in the input box and press `Ctrl+Enter` (`Cmd+Enter` on
  Mac) or select the cloud-upload icon to publish. The list then compares
  against the new staging version.
- The refresh icon re-reads the staging version from the server, for example
  after someone else published.

Only workflow files are compared: `input.js`, `output.js`, `test_input.json`,
`sql/`, and `tests/`. Reference docs, `AGENTS.md`, and `CLAUDE.md` are ignored.
If the workflow was never published, every file shows as added.

## Publishing and Production Versions

1. Wait until the status bar shows `Saved`, then run the workflow and its tests.
2. Review your changes in the Source Control view (see above).
3. In the workflow window, select the cloud-upload title-bar action, run
   **LFM: Publish to Staging** from the command palette, or publish from the
   Source Control view.
4. Enter an optional publish note. The extension publishes the saved draft as
   a new numbered version and reports the version number when it succeeds.
5. Open **LFM: Version History** to review the published version.

Publishing creates a staging version; it does not select that version for
production. To promote a published version, open **Version History**, select
**Set Production** for the intended version, and confirm. The history view
shows which version is marked for staging and which is marked for production.
Treat production selection as a separate release decision.

## Locking

When you open a workflow, the extension acquires an exclusive lock on the
server. A heartbeat keeps the lock alive while the window is open. Closing
the window releases the lock. If another session holds the lock, you'll see
a warning.

## AI Agent Support

Each workflow directory includes generated `AGENTS.md` and `CLAUDE.md` files
that scope AI agents to the workflow's files and provide context about:

- File structure and purpose of each file
- JavaScript runtime constraints (ES5.1 / goja sandbox)
- SQL parameter syntax
- Current queries and test cases
- Pointers to reference documentation

## Development

```bash
cd vscode-extension
npm install
npm run compile     # one-time build
npm run watch       # rebuild on changes
npm run package     # produce .vsix for distribution
```

To test locally, open this directory in VS Code and press `F5` to launch the
Extension Development Host.

## Related Documentation

See the [workflow documentation](https://github.com/fullsend-ai/orgpulse/blob/main/docs/workflows.md) for the dashboard's
workflow model, execution behavior, and related configuration.
