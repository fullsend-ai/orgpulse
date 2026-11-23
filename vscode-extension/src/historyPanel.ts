import * as vscode from "vscode";
import { ApiClient, Workflow, WorkflowVersion } from "./api";
import { WorkflowDiskProvider } from "./fileSystemProvider";
import {
  WEBVIEW_OPTIONS,
  createNonce,
  cspMeta,
  escHtml as esc,
  isCommandMessage,
} from "./webviewUtils";

export class WorkflowHistoryPanel {
  private panel: vscode.WebviewPanel | undefined;
  private workflowId = 0;

  constructor(
    private api: ApiClient,
    private diskProvider: WorkflowDiskProvider,
    private onReverted?: () => void
  ) {}

  async show(wf: Workflow): Promise<void> {
    this.workflowId = wf.id;

    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        "lfmWorkflowHistory",
        `History: ${wf.name}`,
        vscode.ViewColumn.Beside,
        WEBVIEW_OPTIONS
      );
      this.panel.onDidDispose(() => {
        this.panel = undefined;
      });
      this.panel.webview.onDidReceiveMessage((msg: unknown) =>
        this.handleMessage(msg)
      );
    } else {
      this.panel.title = `History: ${wf.name}`;
      this.panel.reveal(vscode.ViewColumn.Beside);
    }

    const webview = this.panel.webview;
    webview.html = buildLoadingHtml(webview);

    try {
      const versions = await this.api.listVersions(wf.id);
      webview.html = buildHistoryHtml(
        webview,
        versions,
        wf.version,
        wf.production_version ?? 0
      );
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      webview.html = buildErrorHtml(webview, msg);
    }
  }

  private async handleMessage(msg: unknown): Promise<void> {
    if (!isCommandMessage(msg)) {
      return;
    }
    if (msg.command === "back") {
      const wf = this.diskProvider.getWorkflow(this.workflowId);
      if (wf) {
        await this.show(wf);
      }
      return;
    }
    const version = msg.version;
    if (typeof version !== "number" || !Number.isInteger(version)) {
      return;
    }
    if (msg.command === "view") {
      await this.viewVersion(version);
    } else if (msg.command === "revert") {
      await this.revertToVersion(version);
    } else if (msg.command === "setProduction") {
      await this.setProductionVersion(version);
    }
  }

  private async viewVersion(version: number): Promise<void> {
    try {
      const ver = await this.api.getVersion(this.workflowId, version);
      if (!this.panel) {
        return;
      }
      this.panel.webview.html = buildVersionDetailHtml(this.panel.webview, ver);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      vscode.window.showErrorMessage(`Failed to load version: ${msg}`);
    }
  }

  private async revertToVersion(version: number): Promise<void> {
    const answer = await vscode.window.showWarningMessage(
      `Revert workflow to version ${version}? This will overwrite the current draft.`,
      { modal: true },
      "Revert"
    );
    if (answer !== "Revert") {
      return;
    }

    try {
      const ver = await this.api.getVersion(this.workflowId, version);
      await this.api.saveDraft(this.workflowId, {
        name: ver.name,
        description: ver.description,
        input_script: ver.input_script,
        output_script: ver.output_script,
        test_input: ver.test_input,
        queries: ver.queries,
        tests: ver.tests,
      });

      const current = this.diskProvider.getWorkflow(this.workflowId);
      if (current) {
        current.input_script = ver.input_script;
        current.output_script = ver.output_script;
        current.test_input = ver.test_input || "{}";
        current.queries = ver.queries || [];
        current.tests = ver.tests || [];
        this.diskProvider.rewriteFiles(this.workflowId);
      }

      if (this.onReverted) {
        this.onReverted();
      }
      vscode.window.showInformationMessage(
        `Reverted to version ${version}. Files updated on disk.`
      );
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      vscode.window.showErrorMessage(`Failed to revert: ${msg}`);
    }
  }

  private async setProductionVersion(version: number): Promise<void> {
    const answer = await vscode.window.showWarningMessage(
      `Set version ${version} as production?`,
      { modal: true },
      "Set Production"
    );
    if (answer !== "Set Production") {
      return;
    }

    try {
      await this.api.setProductionVersion(this.workflowId, version);
      vscode.window.showInformationMessage(
        `Version ${version} is now the production version.`
      );
      const wf = this.diskProvider.getWorkflow(this.workflowId);
      if (wf) {
        wf.production_version = version;
        await this.show(wf);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      vscode.window.showErrorMessage(
        `Failed to set production version: ${msg}`
      );
    }
  }
}

function buildLoadingHtml(webview: vscode.Webview): string {
  return page(webview, `
    <div class="center">
      <div class="spinner"></div>
      Loading version history...
    </div>
  `);
}

function buildErrorHtml(webview: vscode.Webview, msg: string): string {
  return page(webview, `<div class="error-banner">${esc(msg)}</div>`);
}

function buildHistoryHtml(
  webview: vscode.Webview,
  versions: WorkflowVersion[],
  currentVersion: number,
  productionVersion: number
): string {
  if (versions.length === 0) {
    return page(webview, `<div class="center muted">No published versions yet.</div>`);
  }

  const sorted = [...versions].sort((a, b) => b.version - a.version);

  const rows = sorted
    .map((v) => {
      const badges: string[] = [];
      if (v.version === currentVersion) {
        badges.push('<span class="badge staging">staging</span>');
      }
      if (v.version === productionVersion) {
        badges.push('<span class="badge production">production</span>');
      }

      const date = v.published_at
        ? new Date(v.published_at).toLocaleString()
        : "";
      const note = v.publish_note ? esc(v.publish_note) : "";
      const ver = esc(v.version);

      return `<tr>
        <td class="version-num">v${ver}</td>
        <td>${badges.join(" ")}</td>
        <td class="muted">${esc(date)}</td>
        <td class="note">${note}</td>
        <td class="actions">
          <button class="btn" data-action="view" data-version="${ver}" title="View this version">View</button>
          ${v.version !== productionVersion ? `<button class="btn btn-prod" data-action="setProduction" data-version="${ver}" title="Set as production version">Set Production</button>` : ""}
          <button class="btn btn-warn" data-action="revert" data-version="${ver}" title="Revert draft to this version">Revert</button>
        </td>
      </tr>`;
    })
    .join("\n");

  return page(webview, `
    <h2>Version History</h2>
    <table>
      <thead>
        <tr>
          <th>Version</th>
          <th>Status</th>
          <th>Published</th>
          <th>Note</th>
          <th></th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>
  `);
}

function buildVersionDetailHtml(
  webview: vscode.Webview,
  ver: WorkflowVersion
): string {
  const version = esc(ver.version);
  const badges: string[] = [];
  if (ver.production) {
    badges.push('<span class="badge production">production</span>');
  }
  const date = ver.published_at
    ? new Date(ver.published_at).toLocaleString()
    : "";

  const queries = ver.queries || [];
  const stages = new Map<number, typeof queries>();
  for (const q of queries) {
    const s = q.stage ?? 0;
    if (!stages.has(s)) {
      stages.set(s, []);
    }
    stages.get(s)!.push(q);
  }

  let queriesHtml = "";
  for (const [stage, qs] of [...stages.entries()].sort(
    (a, b) => a[0] - b[0]
  )) {
    for (const q of qs) {
      queriesHtml += `
        <details>
          <summary>Step ${esc(stage)}: ${esc(q.label)}</summary>
          <pre>${esc(q.sql_text || "")}</pre>
        </details>`;
    }
  }

  return page(webview, `
    <div class="detail-header">
      <button class="btn" data-action="back">&#8592; Back</button>
      <h2>Version ${version} ${badges.join(" ")}</h2>
    </div>
    <div class="meta">
      ${date ? `<span class="muted">Published: ${esc(date)}</span>` : ""}
      ${ver.publish_note ? `<span class="muted">Note: ${esc(ver.publish_note)}</span>` : ""}
    </div>
    <div class="actions" style="margin:8px 0">
      ${!ver.production ? `<button class="btn btn-prod" data-action="setProduction" data-version="${version}">Set as Production</button>` : ""}
      <button class="btn btn-warn" data-action="revert" data-version="${version}">Revert to this version</button>
    </div>
    <h3>input.js</h3>
    <pre>${esc(ver.input_script || "")}</pre>
    <h3>output.js</h3>
    <pre>${esc(ver.output_script || "")}</pre>
    ${queriesHtml ? `<h3>SQL Queries</h3>${queriesHtml}` : ""}
  `);
}

function page(webview: vscode.Webview, body: string): string {
  const nonce = createNonce();
  return `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
${cspMeta(webview, nonce)}
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: var(--vscode-font-family, sans-serif);
    font-size: var(--vscode-font-size, 13px);
    color: var(--vscode-foreground);
    background: var(--vscode-editor-background);
    padding: 12px 16px;
    overflow: auto;
  }
  h2 { font-size: 15px; font-weight: 600; margin-bottom: 12px; display: inline; }
  h3 { font-size: 13px; font-weight: 600; margin: 14px 0 6px; }
  table { width: 100%; border-collapse: collapse; }
  th {
    text-align: left;
    font-size: 11px;
    text-transform: uppercase;
    color: var(--vscode-descriptionForeground, #888);
    padding: 6px 8px;
    border-bottom: 1px solid var(--vscode-panel-border, #333);
  }
  td {
    padding: 8px;
    border-bottom: 1px solid var(--vscode-panel-border, #333);
    vertical-align: middle;
  }
  .version-num { font-weight: 600; font-variant-numeric: tabular-nums; }
  .muted { color: var(--vscode-descriptionForeground, #888); font-size: 12px; }
  .note { font-size: 12px; max-width: 300px; }
  .badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 8px;
    font-size: 11px;
    font-weight: 600;
  }
  .badge.staging {
    background: var(--vscode-debugIcon-startForeground, #388a34);
    color: #fff;
  }
  .badge.production {
    background: var(--vscode-charts-blue, #1e7cd6);
    color: #fff;
  }
  .actions { white-space: nowrap; }
  .btn {
    padding: 3px 10px;
    border: 1px solid var(--vscode-button-border, var(--vscode-panel-border, #555));
    background: var(--vscode-button-secondaryBackground, #333);
    color: var(--vscode-button-secondaryForeground, #ccc);
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
    margin-right: 4px;
  }
  .btn:hover {
    background: var(--vscode-button-secondaryHoverBackground, #444);
  }
  .btn-warn {
    background: var(--vscode-inputValidation-warningBackground, #5a4000);
    color: var(--vscode-inputValidation-warningForeground, #fff);
    border-color: var(--vscode-inputValidation-warningBorder, #b89500);
  }
  .btn-warn:hover { opacity: 0.9; }
  .btn-prod {
    background: var(--vscode-charts-blue, #1e7cd6);
    color: #fff;
    border-color: var(--vscode-charts-blue, #1e7cd6);
  }
  .btn-prod:hover { opacity: 0.9; }
  pre {
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--vscode-editor-font-family, monospace);
    font-size: var(--vscode-editor-font-size, 13px);
    line-height: 1.5;
    background: var(--vscode-textCodeBlock-background, #1e1e1e);
    padding: 8px 10px;
    border-radius: 4px;
    margin-bottom: 4px;
  }
  details { margin: 4px 0; }
  summary {
    cursor: pointer;
    font-size: 12px;
    color: var(--vscode-textLink-foreground, #3794ff);
    margin-bottom: 4px;
  }
  .center {
    display: flex; align-items: center; justify-content: center;
    gap: 10px; padding: 40px;
    color: var(--vscode-descriptionForeground, #888);
  }
  .error-banner { color: var(--vscode-errorForeground, #f14c4c); padding: 16px; }
  .spinner {
    width: 18px; height: 18px;
    border: 2px solid var(--vscode-descriptionForeground, #888);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  .detail-header { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  .meta { display: flex; gap: 16px; margin-bottom: 4px; }
</style>
</head>
<body>
${body}
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  function view(v) { vscode.postMessage({ command: 'view', version: v }); }
  function revert(v) { vscode.postMessage({ command: 'revert', version: v }); }
  function setProduction(v) { vscode.postMessage({ command: 'setProduction', version: v }); }
  function back() { vscode.postMessage({ command: 'back' }); }
  const actions = { view, revert, setProduction };
  document.addEventListener('click', (e) => {
    const btn = e.target instanceof Element ? e.target.closest('button[data-action]') : null;
    if (!btn) return;
    const action = btn.dataset.action;
    if (action === 'back') {
      back();
    } else if (Object.prototype.hasOwnProperty.call(actions, action)) {
      actions[action](Number(btn.dataset.version));
    }
  });
</script>
</body>
</html>`;
}
