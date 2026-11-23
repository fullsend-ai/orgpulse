import * as vscode from "vscode";
import * as fs from "fs";
import * as path from "path";
import * as http from "http";
import * as crypto from "crypto";
import { ApiClient, WorkflowQuery } from "./api";
import { WorkflowDiskProvider, readMarkerFile } from "./fileSystemProvider";
import { LockManager } from "./lockManager";
import { WorkflowResultPanel } from "./resultPanel";
import { WorkflowHistoryPanel } from "./historyPanel";
import { WorkflowStatusBar } from "./statusBar";
import { DevLakeSqlPanel } from "./sqlPanel";
import { StagingScm } from "./scm";

// SecretStorage key for the API key. The `lfm.apiKey` setting remains a
// supported fallback for manual or scripted configuration.
const API_KEY_SECRET = "lfm.apiKey";

export async function activate(
  context: vscode.ExtensionContext
): Promise<void> {
  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.login", () => loginWithBrowser(context))
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.logout", () => clearStoredApiKey(context))
  );

  const config = vscode.workspace.getConfiguration("lfm");
  const serverUrl = config.get<string>("serverUrl", "http://localhost:9090");
  const storedKey = await readStoredApiKey(context);
  const settingKey = config.get<string>("apiKey", "");
  const apiKey = storedKey || settingKey;

  if (!storedKey && settingKey) {
    offerApiKeyMigration(context, settingKey);
  }

  if (!apiKey) {
    vscode.window
      .showWarningMessage(
        "LFM: No API key configured. Log in to generate one.",
        "Login"
      )
      .then((action) => {
        if (action === "Login") {
          vscode.commands.executeCommand("lfm.login");
        }
      });
  }

  const api = new ApiClient(serverUrl, apiKey);
  registerSqlCommands(context, api);

  const workspaceRoot =
    vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  const marker = workspaceRoot ? readMarkerFile(workspaceRoot) : undefined;

  if (marker) {
    activateWorkflowMode(context, api, marker.id, marker.name);
  } else {
    activateBrowseMode(context, api);
  }

  const promptReload = () => {
    vscode.window
      .showInformationMessage(
        "LFM: Configuration changed. Reload window to apply.",
        "Reload"
      )
      .then((action) => {
        if (action === "Reload") {
          vscode.commands.executeCommand("workbench.action.reloadWindow");
        }
      });
  };

  // The API client keeps the key it was created with, so prompt for a reload
  // whenever the effective key (secret, else setting) differs from it. This
  // also avoids a spurious prompt when a key moves from settings to secrets.
  const promptIfApiKeyChanged = async () => {
    const effective =
      (await readStoredApiKey(context)) ||
      vscode.workspace.getConfiguration("lfm").get<string>("apiKey", "");
    if (effective !== apiKey) {
      promptReload();
    }
  };

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("lfm.serverUrl")) {
        promptReload();
      } else if (e.affectsConfiguration("lfm.apiKey")) {
        void promptIfApiKeyChanged();
      }
    })
  );

  context.subscriptions.push(
    context.secrets.onDidChange((e) => {
      if (e.key === API_KEY_SECRET) {
        void promptIfApiKeyChanged();
      }
    })
  );
}

async function readStoredApiKey(
  context: vscode.ExtensionContext
): Promise<string> {
  try {
    return (await context.secrets.get(API_KEY_SECRET)) || "";
  } catch {
    // SecretStorage can be unavailable (e.g. no OS keyring); fall back to settings.
    return "";
  }
}

// Offers to move a plaintext `lfm.apiKey` setting into SecretStorage. The
// setting is only cleared after the user agrees and the secret is stored.
function offerApiKeyMigration(
  context: vscode.ExtensionContext,
  settingKey: string
): void {
  const move = "Move to Secure Storage";
  vscode.window
    .showInformationMessage(
      "LFM: Your API key is stored in plaintext in settings.json. Move it to VS Code's secure storage?",
      move,
      "Not Now"
    )
    .then(async (action) => {
      if (action !== move) {
        return;
      }
      try {
        await context.secrets.store(API_KEY_SECRET, settingKey);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(
          `LFM: Could not store the API key securely: ${msg}. The setting was left unchanged.`
        );
        return;
      }
      await clearApiKeySetting();
    });
}

// Removes `lfm.apiKey` from every settings scope where it is set.
async function clearApiKeySetting(): Promise<void> {
  const failures: string[] = [];
  const clear = async (
    config: vscode.WorkspaceConfiguration,
    target: vscode.ConfigurationTarget,
    label: string
  ) => {
    try {
      await config.update("apiKey", undefined, target);
    } catch {
      failures.push(label);
    }
  };

  const config = vscode.workspace.getConfiguration("lfm");
  const info = config.inspect<string>("apiKey");
  if (info?.globalValue !== undefined) {
    await clear(config, vscode.ConfigurationTarget.Global, "User");
  }
  if (info?.workspaceValue !== undefined) {
    await clear(config, vscode.ConfigurationTarget.Workspace, "Workspace");
  }
  for (const folder of vscode.workspace.workspaceFolders ?? []) {
    const folderConfig = vscode.workspace.getConfiguration("lfm", folder.uri);
    if (folderConfig.inspect<string>("apiKey")?.workspaceFolderValue !== undefined) {
      await clear(
        folderConfig,
        vscode.ConfigurationTarget.WorkspaceFolder,
        `Folder "${folder.name}"`
      );
    }
  }

  if (failures.length > 0) {
    vscode.window.showWarningMessage(
      `LFM: The API key was stored securely, but the lfm.apiKey setting could not be removed from: ${failures.join(", ")}. Remove it manually.`
    );
  } else {
    vscode.window.showInformationMessage(
      "LFM: API key moved to secure storage."
    );
  }
}

async function clearStoredApiKey(
  context: vscode.ExtensionContext
): Promise<void> {
  try {
    await context.secrets.delete(API_KEY_SECRET);
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    vscode.window.showErrorMessage(`LFM: Could not clear the stored API key: ${msg}`);
    return;
  }
  const settingKey = vscode.workspace
    .getConfiguration("lfm")
    .get<string>("apiKey", "");
  vscode.window.showInformationMessage(
    settingKey
      ? "LFM: Stored API key cleared. The lfm.apiKey setting is still set and will be used."
      : "LFM: Stored API key cleared."
  );
}

function activateBrowseMode(
  context: vscode.ExtensionContext,
  api: ApiClient
): void {
  const diskProvider = new WorkflowDiskProvider(api);

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.openWorkflow", async () => {
      try {
        const workflows = await api.listWorkflows();
        if (workflows.length === 0) {
          vscode.window.showInformationMessage(
            "No workflows found. Use 'LFM: Create Workflow' to create one."
          );
          return;
        }

        const items = workflows.map((wf) => ({
          label: wf.name,
          description: wf.locked ? "$(lock) locked" : "",
          detail: wf.description || "",
          workflowId: wf.id,
        }));

        const picked = await vscode.window.showQuickPick(items, {
          placeHolder: "Select a workflow to open",
          matchOnDescription: true,
          matchOnDetail: true,
        });
        if (!picked) {
          return;
        }

        await openWorkflowInNewWindow(api, diskProvider, picked.workflowId);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`Failed to list workflows: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.createWorkflow", async () => {
      const name = await vscode.window.showInputBox({
        prompt: "Workflow name",
        placeHolder: "e.g. team-metrics",
        validateInput: (v) => {
          if (!v.trim()) {
            return "Name is required";
          }
          if (!/^[a-z0-9][a-z0-9_-]*$/.test(v.trim())) {
            return "Use lowercase letters, digits, hyphens, and underscores (must start with a letter or digit)";
          }
          return null;
        },
      });
      if (!name) {
        return;
      }
      const description = await vscode.window.showInputBox({
        prompt: "Description (optional)",
        placeHolder: "What does this workflow do?",
      });
      try {
        const wf = await api.createWorkflow({
          name: name.trim(),
          description: description?.trim() || undefined,
          input_script:
            "// Transform the HTTP request body into named SQL parameters.\n// The global variable `input` contains the parsed JSON body.\n// The last expression is used as the return value (ES5.1, no `return`).\n\n({\n  \n})\n",
          output_script:
            "// Transform query results into the final HTTP response.\n// Globals: `input` (from input.js), `results` (map of query label -> rows).\n// The last expression is used as the return value (ES5.1, no `return`).\n\n({\n  \n})\n",
          queries: defaultQueries(),
        });
        await openWorkflowInNewWindow(api, diskProvider, wf.id);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(
          `Failed to create workflow: ${msg}`
        );
      }
    })
  );

  registerNoOpCommands(context, [
    "lfm.runWorkflow",
    "lfm.runTests",
    "lfm.addTest",
    "lfm.deleteTest",
    "lfm.publishWorkflow",
    "lfm.showHistory",
    "lfm.refreshDocs",
    "lfm.showOutput",
    "lfm.publishFromScm",
    "lfm.scm.refresh",
    "lfm.scm.openChange",
    "lfm.scm.openFile",
  ]);

  context.subscriptions.push(
    new vscode.Disposable(() => {
      diskProvider.dispose();
    })
  );
}

async function openWorkflowInNewWindow(
  api: ApiClient,
  diskProvider: WorkflowDiskProvider,
  workflowId: number
): Promise<void> {
  const wf = await api.getWorkflow(workflowId);
  const dir = diskProvider.workflowDir(wf.id, wf.name);

  fs.mkdirSync(dir, { recursive: true });
  diskProvider.writeMarkerFile(wf);

  await vscode.commands.executeCommand(
    "vscode.openFolder",
    vscode.Uri.file(dir),
    { forceNewWindow: true }
  );
}

function activateWorkflowMode(
  context: vscode.ExtensionContext,
  api: ApiClient,
  workflowId: number,
  workflowName: string
): void {
  vscode.commands.executeCommand(
    "setContext",
    "lfm.workflowMode",
    true
  );

  const outputChannel =
    vscode.window.createOutputChannel("LFM Workflows");
  context.subscriptions.push(outputChannel);

  const statusBar = new WorkflowStatusBar(workflowName, outputChannel);
  context.subscriptions.push(statusBar);

  const lockMgr = new LockManager(api);
  const diskProvider = new WorkflowDiskProvider(api);

  diskProvider.onSaveStart = () => statusBar.setSaving();
  diskProvider.onSaveEnd = (error) => {
    if (error) {
      statusBar.setError(error);
      outputChannel.appendLine(`[ERROR] Save failed: ${error}`);
    } else {
      statusBar.setSaved();
    }
  };
  diskProvider.onWarning = (message) => {
    outputChannel.appendLine(`[WARN] ${message}`);
  };

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.showOutput", () => {
      outputChannel.show();
    })
  );

  lockMgr
    .acquire(workflowId)
    .then((acquired) => {
      if (!acquired) {
        vscode.window.showWarningMessage(
          `Workflow "${workflowName}" is locked by another session. ` +
            "Edits may not save until the lock is released."
        );
        outputChannel.appendLine(
          `[WARN] Could not acquire lock for workflow ${workflowId}`
        );
      } else {
        outputChannel.appendLine(
          `Acquired lock for workflow "${workflowName}" (id=${workflowId})`
        );
      }
    })
    .catch(() => {});

  diskProvider
    .loadWorkflow(workflowId)
    .then((wf) => {
      outputChannel.appendLine(
        `Loaded workflow "${wf.name}" v${wf.version}`
      );
      registerWorkflowCommands(
        context,
        api,
        diskProvider,
        workflowId,
        outputChannel
      );
    })
    .catch((err: Error) => {
      vscode.window.showErrorMessage(
        `Failed to load workflow: ${err.message}`
      );
      outputChannel.appendLine(`[ERROR] Load failed: ${err.message}`);
    });

  // Also register browse commands so users can open other workflows
  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.openWorkflow", async () => {
      try {
        const workflows = await api.listWorkflows();
        const items = workflows.map((wf) => ({
          label: wf.name,
          description: wf.locked ? "$(lock) locked" : "",
          detail: wf.description || "",
          workflowId: wf.id,
        }));

        const picked = await vscode.window.showQuickPick(items, {
          placeHolder: "Select a workflow to open",
          matchOnDescription: true,
          matchOnDetail: true,
        });
        if (!picked) {
          return;
        }

        const tempProvider = new WorkflowDiskProvider(api);
        await openWorkflowInNewWindow(api, tempProvider, picked.workflowId);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`Failed to list workflows: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.createWorkflow", async () => {
      const name = await vscode.window.showInputBox({
        prompt: "Workflow name",
        placeHolder: "e.g. team-metrics",
        validateInput: (v) => {
          if (!v.trim()) {
            return "Name is required";
          }
          if (!/^[a-z0-9][a-z0-9_-]*$/.test(v.trim())) {
            return "Use lowercase letters, digits, hyphens, and underscores (must start with a letter or digit)";
          }
          return null;
        },
      });
      if (!name) {
        return;
      }
      const description = await vscode.window.showInputBox({
        prompt: "Description (optional)",
        placeHolder: "What does this workflow do?",
      });
      try {
        const wf = await api.createWorkflow({
          name: name.trim(),
          description: description?.trim() || undefined,
          input_script:
            "// Transform the HTTP request body into named SQL parameters.\n// The global variable `input` contains the parsed JSON body.\n// The last expression is used as the return value (ES5.1, no `return`).\n\n({\n  \n})\n",
          output_script:
            "// Transform query results into the final HTTP response.\n// Globals: `input` (from input.js), `results` (map of query label -> rows).\n// The last expression is used as the return value (ES5.1, no `return`).\n\n({\n  \n})\n",
          queries: defaultQueries(),
        });
        const tempProvider = new WorkflowDiskProvider(api);
        await openWorkflowInNewWindow(api, tempProvider, wf.id);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(
          `Failed to create workflow: ${msg}`
        );
      }
    })
  );

  context.subscriptions.push(
    new vscode.Disposable(() => {
      lockMgr.releaseAll().catch(() => {});
      lockMgr.dispose();
      diskProvider.dispose();
    })
  );
}

function registerWorkflowCommands(
  context: vscode.ExtensionContext,
  api: ApiClient,
  diskProvider: WorkflowDiskProvider,
  workflowId: number,
  outputChannel: vscode.OutputChannel
): void {
  const resultPanel = new WorkflowResultPanel(api, context.extensionUri);
  const historyPanel = new WorkflowHistoryPanel(api, diskProvider);

  const loaded = diskProvider.getWorkflow(workflowId);
  const stagingScm = loaded
    ? new StagingScm(api, workflowId, diskProvider.workflowDir(loaded.id, loaded.name), loaded.version)
    : undefined;
  if (stagingScm) {
    context.subscriptions.push(stagingScm);
  }

  const publish = async (note: string): Promise<boolean> => {
    const wf = diskProvider.getWorkflow(workflowId);
    if (!wf) {
      vscode.window.showErrorMessage("Workflow not loaded yet.");
      return false;
    }

    await vscode.workspace.saveAll(false);
    await diskProvider.flushSaves(workflowId);

    try {
      const published = await api.publishWorkflow(
        wf.id,
        note.trim() || undefined
      );
      wf.version = published.version;
      stagingScm?.setStagingVersion(published.version);
      outputChannel.appendLine(
        `Published workflow "${wf.name}" as v${published.version}`
      );
      vscode.window.showInformationMessage(
        `Workflow published as v${published.version}.`
      );
      return true;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      outputChannel.appendLine(`[ERROR] Publish failed: ${msg}`);
      vscode.window.showErrorMessage(`Failed to publish workflow: ${msg}`);
      return false;
    }
  };

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.runWorkflow", async () => {
      const wf = diskProvider.getWorkflow(workflowId);
      if (!wf) {
        vscode.window.showErrorMessage("Workflow not loaded yet.");
        return;
      }
      await vscode.workspace.saveAll(false);
      await diskProvider.flushSaves(workflowId);
      await resultPanel.run(wf);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.runTests", async () => {
      const wf = diskProvider.getWorkflow(workflowId);
      if (!wf) {
        vscode.window.showErrorMessage("Workflow not loaded yet.");
        return;
      }
      const tests = wf.tests || [];
      if (tests.length === 0) {
        vscode.window.showWarningMessage(
          "No tests defined. Use 'LFM: Add Test' to create one."
        );
        return;
      }

      await vscode.workspace.saveAll(false);
      await diskProvider.flushSaves(workflowId);

      outputChannel.clear();
      outputChannel.show(true);
      outputChannel.appendLine(
        `Running ${tests.length} test(s) for workflow "${wf.name}" (id=${wf.id})...`
      );
      outputChannel.appendLine("");

      try {
        const resp = await api.runTests(wf.id);
        const results = resp.results || [];
        let passed = 0;
        let failed = 0;

        for (const r of results) {
          if (r.passed) {
            passed++;
            outputChannel.appendLine(`  ✓ ${r.name}`);
          } else {
            failed++;
            outputChannel.appendLine(`  ✗ ${r.name}`);
            if (r.error) {
              outputChannel.appendLine(`    ${r.error}`);
            }
          }
        }

        outputChannel.appendLine("");
        outputChannel.appendLine(
          `Results: ${passed} passed, ${failed} failed, ${results.length} total`
        );

        if (failed > 0) {
          vscode.window.showWarningMessage(
            `${failed} of ${results.length} test(s) failed.`
          );
        } else {
          vscode.window.showInformationMessage(
            `All ${results.length} test(s) passed.`
          );
        }
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        outputChannel.appendLine(`Error: ${msg}`);
        vscode.window.showErrorMessage(`Failed to run tests: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.addTest", async () => {
      const name = await vscode.window.showInputBox({
        prompt: "Test case name",
        placeHolder: "e.g. basic_team_query",
        validateInput: (v) => (v.trim() ? null : "Name is required"),
      });
      if (!name) {
        return;
      }
      if (diskProvider.addTest(workflowId, name.trim())) {
        vscode.window.showInformationMessage(
          `Test "${name.trim()}" created. Edit input.json and assert.js.`
        );
      } else {
        vscode.window.showWarningMessage(
          `Test "${name.trim()}" already exists.`
        );
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.deleteTest", async () => {
      const wf = diskProvider.getWorkflow(workflowId);
      if (!wf || !wf.tests || wf.tests.length === 0) {
        vscode.window.showWarningMessage("No tests to delete.");
        return;
      }

      const items = wf.tests.map((t) => ({
        label: t.name,
      }));

      const picked = await vscode.window.showQuickPick(items, {
        placeHolder: "Select a test to delete",
      });
      if (!picked) {
        return;
      }

      const answer = await vscode.window.showWarningMessage(
        `Delete test "${picked.label}"?`,
        { modal: true },
        "Delete"
      );
      if (answer !== "Delete") {
        return;
      }

      if (diskProvider.deleteTest(workflowId, picked.label)) {
        vscode.window.showInformationMessage(
          `Test "${picked.label}" deleted.`
        );
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.publishWorkflow", async () => {
      const note = await vscode.window.showInputBox({
        prompt: "Publish note (optional)",
        placeHolder: "e.g. Added team filter support",
        value: stagingScm?.sourceControl.inputBox.value ?? "",
      });
      if (note === undefined) {
        return;
      }
      if (await publish(note) && stagingScm) {
        stagingScm.sourceControl.inputBox.value = "";
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.publishFromScm", async () => {
      if (!stagingScm) {
        return;
      }
      if (await publish(stagingScm.sourceControl.inputBox.value)) {
        stagingScm.sourceControl.inputBox.value = "";
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.scm.refresh", () => stagingScm?.refreshFromServer())
  );

  // Invoked with a Uri when a change is clicked, and with the resource state from context menus.
  const resourceUri = (arg?: vscode.Uri | vscode.SourceControlResourceState) =>
    arg instanceof vscode.Uri ? arg : arg?.resourceUri;

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.scm.openChange", (arg?: vscode.Uri | vscode.SourceControlResourceState) => {
      const uri = resourceUri(arg);
      return uri && stagingScm?.openChange(uri);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.scm.openFile", (arg?: vscode.Uri | vscode.SourceControlResourceState) => {
      const uri = resourceUri(arg);
      return uri && vscode.commands.executeCommand("vscode.open", uri);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.showHistory", async () => {
      const wf = diskProvider.getWorkflow(workflowId);
      if (!wf) {
        vscode.window.showErrorMessage("Workflow not loaded yet.");
        return;
      }
      await historyPanel.show(wf);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.refreshDocs", async () => {
      try {
        await diskProvider.loadDocs(workflowId);
        vscode.window.showInformationMessage(
          "LFM: Reference docs refreshed."
        );
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(
          `Failed to refresh docs: ${msg}`
        );
      }
    })
  );
}

function registerSqlCommands(
  context: vscode.ExtensionContext,
  api: ApiClient
): void {
  const sqlPanel = new DevLakeSqlPanel(api);

  // Other SQL extensions add their own "Run" buttons to SQL editors; a CodeLens
  // makes the DevLake action unambiguous.
  context.subscriptions.push(
    vscode.languages.registerCodeLensProvider(
      { language: "sql" },
      {
        provideCodeLenses: () => [
          new vscode.CodeLens(new vscode.Range(0, 0, 0, 0), {
            title: "$(database) Run on DevLake",
            tooltip: "Run the selection or the whole file against the DevLake database",
            command: "lfm.runSql",
          }),
        ],
      }
    )
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.runSql", async () => {
      const editor = vscode.window.activeTextEditor;
      let sql: string | undefined;
      if (editor) {
        sql = editor.selection.isEmpty
          ? editor.document.getText()
          : editor.document.getText(editor.selection);
      }
      if (!sql?.trim()) {
        sql = await vscode.window.showInputBox({
          prompt: "SQL to execute against the DevLake database",
          placeHolder: "SELECT * FROM issues LIMIT 10",
        });
      }
      if (sql?.trim()) {
        await sqlPanel.run(sql);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.newSqlQuery", async () => {
      await openSqlEditor(
        "-- Runs against the DevLake database.\n" +
          "-- Run the selection or the whole file with 'LFM: Run SQL on DevLake'.\n" +
          "SELECT 1\n"
      );
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("lfm.browseDevLakeTables", async () => {
      try {
        const tables = await api.devlakeTables();
        if (tables.length === 0) {
          vscode.window.showInformationMessage("No DevLake tables found.");
          return;
        }
        const picked = await vscode.window.showQuickPick(tables, {
          placeHolder: "Select a DevLake table to preview",
        });
        if (!picked) {
          return;
        }
        const sql = `SELECT *\nFROM \`${picked.replace(/`/g, "``")}\`\nLIMIT 100\n`;
        await openSqlEditor(sql);
        await sqlPanel.run(sql);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`Failed to list DevLake tables: ${msg}`);
      }
    })
  );
}

async function openSqlEditor(content: string): Promise<void> {
  const doc = await vscode.workspace.openTextDocument({
    language: "sql",
    content,
  });
  await vscode.window.showTextDocument(doc, vscode.ViewColumn.Active);
}

function defaultQueries(): WorkflowQuery[] {
  return [
    {
      label: "main",
      stage: 0,
      sort_order: 0,
      sql_text:
        "-- Stage 0: queries in this folder run in parallel.\n" +
        "-- Use :paramName to reference values from input.js.\n" +
        "-- Rename this file to change the query label.\n" +
        "SELECT 1\n",
    },
  ];
}

async function loginWithBrowser(
  context: vscode.ExtensionContext
): Promise<void> {
  const config = vscode.workspace.getConfiguration("lfm");
  const serverUrl = config.get<string>("serverUrl", "http://localhost:9090");

  if (!serverUrl || serverUrl === "http://localhost:9090") {
    const url = await vscode.window.showInputBox({
      prompt: "LFM Server URL",
      placeHolder: "https://your-dashboard.example.com",
      validateInput: (v) => {
        try {
          new URL(v.trim());
          return null;
        } catch {
          return "Enter a valid URL";
        }
      },
    });
    if (!url) {
      return;
    }
    await config.update("serverUrl", url.trim(), vscode.ConfigurationTarget.Global);
  }

  const finalServerUrl = vscode.workspace
    .getConfiguration("lfm")
    .get<string>("serverUrl", serverUrl);

  const token = await waitForTokenCallback(finalServerUrl);
  if (!token) {
    return;
  }

  try {
    await context.secrets.store(API_KEY_SECRET, token);
  } catch {
    // Without a usable SecretStorage, keep the previous behaviour.
    await config.update("apiKey", token, vscode.ConfigurationTarget.Global);
    vscode.window.showWarningMessage(
      "LFM: Secure storage is unavailable; the API key was saved to the lfm.apiKey setting."
    );
  }
  vscode.window
    .showInformationMessage("LFM: Logged in successfully. Reload to apply.", "Reload")
    .then((action) => {
      if (action === "Reload") {
        vscode.commands.executeCommand("workbench.action.reloadWindow");
      }
    });
}

function waitForTokenCallback(serverUrl: string): Promise<string | undefined> {
  // Random per-login state; the server echoes it back on the callback so that
  // only the redirect for this login attempt is accepted.
  const state = crypto.randomBytes(32).toString("base64url");
  const expectedState = Buffer.from(state);

  return new Promise((resolve) => {
    let done = false;
    const finish = (token: string | undefined) => {
      done = true;
      server.close();
      clearTimeout(timeout);
      resolve(token);
    };

    const server = http.createServer((req, res) => {
      const url = new URL(req.url || "/", `http://localhost`);
      const headers = {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
      };

      // Ignore unrelated requests (e.g. /favicon.ico) and keep waiting for
      // the real callback.
      if (done || req.method !== "GET" || url.pathname !== "/callback") {
        res.writeHead(404, headers);
        res.end("<html><body><h2>Not found</h2></body></html>");
        return;
      }

      const receivedState = Buffer.from(url.searchParams.get("state") || "");
      if (
        receivedState.length !== expectedState.length ||
        !crypto.timingSafeEqual(receivedState, expectedState)
      ) {
        res.writeHead(400, headers);
        res.end(
          "<html><body><h2>Login failed</h2>" +
            "<p>Invalid or missing login state. Start the login again from VS Code.</p></body></html>"
        );
        return;
      }

      const token = url.searchParams.get("token");
      res.writeHead(200, headers);
      if (token) {
        res.end(
          "<html><body><h2>Login successful!</h2>" +
            "<p>You can close this tab and return to VS Code.</p>" +
            "<script>window.close()</script></body></html>"
        );
      } else {
        res.end(
          "<html><body><h2>Login failed</h2>" +
            "<p>No token received. Please try again.</p></body></html>"
        );
      }

      finish(token || undefined);
    });

    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (!addr || typeof addr === "string") {
        finish(undefined);
        return;
      }
      const callbackUrl = `http://127.0.0.1:${addr.port}/callback`;
      const loginUrl =
        `${serverUrl}/auth/vscode-token?callback=${encodeURIComponent(callbackUrl)}` +
        `&state=${encodeURIComponent(state)}`;
      vscode.env.openExternal(vscode.Uri.parse(loginUrl));
    });

    const timeout = setTimeout(() => {
      if (done) {
        return;
      }
      finish(undefined);
      vscode.window.showWarningMessage("LFM: Login timed out. Try again.");
    }, 120_000);
  });
}

function registerNoOpCommands(
  context: vscode.ExtensionContext,
  commands: string[]
): void {
  for (const cmd of commands) {
    context.subscriptions.push(
      vscode.commands.registerCommand(cmd, () => {})
    );
  }
}

export function deactivate(): void {
  // cleanup handled by disposables
}
