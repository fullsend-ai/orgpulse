import * as vscode from "vscode";
import { ApiClient, DevLakeQueryResult } from "./api";
import {
  WEBVIEW_OPTIONS,
  createNonce,
  cspMeta,
  escHtml,
  isCommandMessage,
} from "./webviewUtils";

// Rendering very large result sets as HTML makes the webview sluggish; the
// full result stays available through the JSON/CSV export actions.
const MAX_RENDERED_ROWS = 1000;

const READ_ONLY_KEYWORDS = new Set([
  "SELECT",
  "SHOW",
  "DESCRIBE",
  "DESC",
  "EXPLAIN",
  "WITH",
]);

export class DevLakeSqlPanel {
  private panel: vscode.WebviewPanel | undefined;
  private lastResult: DevLakeQueryResult | undefined;

  constructor(private api: ApiClient) {}

  async run(sql: string): Promise<void> {
    const query = sql.trim();
    if (!query) {
      vscode.window.showWarningMessage("No SQL to execute.");
      return;
    }

    if (!isReadOnly(query)) {
      const answer = await vscode.window.showWarningMessage(
        "This statement may modify the DevLake database. Execute it anyway?",
        { modal: true, detail: firstLine(query) },
        "Execute"
      );
      if (answer !== "Execute") {
        return;
      }
    }

    this.createOrShowPanel();
    this.lastResult = undefined;
    this.render(query, { running: true });

    const start = Date.now();
    try {
      const result = await this.api.devlakeQuery(query);
      this.lastResult = result;
      this.render(query, { result, elapsedMs: Date.now() - start });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      this.render(query, { error: msg, elapsedMs: Date.now() - start });
    }
  }

  private createOrShowPanel(): void {
    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Beside, true);
      return;
    }

    this.panel = vscode.window.createWebviewPanel(
      "lfmDevLakeSql",
      "DevLake SQL",
      { viewColumn: vscode.ViewColumn.Beside, preserveFocus: true },
      WEBVIEW_OPTIONS
    );

    this.panel.onDidDispose(() => {
      this.panel = undefined;
      this.lastResult = undefined;
    });

    this.panel.webview.onDidReceiveMessage((msg: unknown) =>
      this.handleMessage(msg)
    );
  }

  private async handleMessage(msg: unknown): Promise<void> {
    if (
      !isCommandMessage(msg) ||
      (msg.command !== "openJson" && msg.command !== "openCsv")
    ) {
      return;
    }
    const result = this.lastResult;
    if (!result) {
      return;
    }
    const doc =
      msg.command === "openCsv"
        ? await vscode.workspace.openTextDocument({
            language: "csv",
            content: toCsv(result),
          })
        : await vscode.workspace.openTextDocument({
            language: "json",
            content: JSON.stringify(result.rows, null, 2),
          });
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.Active);
  }

  private render(
    sql: string,
    state: {
      running?: boolean;
      result?: DevLakeQueryResult;
      error?: string;
      elapsedMs?: number;
    }
  ): void {
    if (!this.panel) {
      return;
    }

    let body: string;
    if (state.running) {
      body =
        '<div class="running"><div class="spinner"></div>Executing query...</div>';
    } else if (state.error !== undefined) {
      body = `<div class="error-banner">${escHtml(state.error)}</div>`;
    } else {
      body = buildResultHtml(state.result);
    }

    const meta: string[] = [];
    if (state.result) {
      const n = state.result.rows.length;
      meta.push(`${n} row${n === 1 ? "" : "s"}`);
    }
    if (state.elapsedMs !== undefined) {
      meta.push(`${state.elapsedMs} ms`);
    }
    const hasRows = !!state.result && state.result.rows.length > 0;
    const actions = hasRows
      ? `<button data-command="openJson">Open as JSON</button>
         <button data-command="openCsv">Open as CSV</button>`
      : "";

    const nonce = createNonce();
    this.panel.webview.html = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
${cspMeta(this.panel.webview, nonce)}
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: var(--vscode-font-family, sans-serif);
    font-size: var(--vscode-font-size, 13px);
    color: var(--vscode-foreground);
    background: var(--vscode-editor-background);
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }
  .header {
    flex-shrink: 0;
    padding: 8px 12px;
    border-bottom: 1px solid var(--vscode-panel-border, #333);
  }
  details summary {
    cursor: pointer;
    color: var(--vscode-textLink-foreground, #3794ff);
    font-size: 12px;
  }
  pre.sql {
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--vscode-editor-font-family, monospace);
    font-size: var(--vscode-editor-font-size, 13px);
    color: var(--vscode-descriptionForeground, #888);
    margin-top: 4px;
    max-height: 160px;
    overflow: auto;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 6px;
  }
  .meta { color: var(--vscode-descriptionForeground, #888); font-size: 12px; margin-right: auto; }
  button {
    background: var(--vscode-button-secondaryBackground, #3a3d41);
    color: var(--vscode-button-secondaryForeground, #fff);
    border: none;
    padding: 3px 10px;
    font-size: 12px;
    cursor: pointer;
    border-radius: 2px;
  }
  button:hover { background: var(--vscode-button-secondaryHoverBackground, #45494e); }
  .content { flex: 1; overflow: auto; }
  table { border-collapse: collapse; font-family: var(--vscode-editor-font-family, monospace); font-size: 12px; }
  th, td {
    text-align: left;
    padding: 3px 10px;
    border-bottom: 1px solid var(--vscode-panel-border, #333);
    white-space: nowrap;
    max-width: 480px;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: top;
  }
  th {
    position: sticky;
    top: 0;
    background: var(--vscode-editorGroupHeader-tabsBackground, #252526);
    font-weight: 600;
  }
  tr:hover td { background: var(--vscode-list-hoverBackground, #2a2d2e); }
  td.null { color: var(--vscode-descriptionForeground, #888); font-style: italic; }
  .note, .empty {
    color: var(--vscode-descriptionForeground, #888);
    padding: 12px;
  }
  .empty { text-align: center; padding: 24px; }
  .error-banner {
    color: var(--vscode-errorForeground, #f14c4c);
    padding: 16px;
    white-space: pre-wrap;
    font-family: var(--vscode-editor-font-family, monospace);
  }
  .running {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 40px;
    color: var(--vscode-descriptionForeground, #888);
  }
  .spinner {
    width: 18px; height: 18px;
    border: 2px solid var(--vscode-descriptionForeground, #888);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
<div class="header">
  <details><summary>Query</summary><pre class="sql">${escHtml(sql)}</pre></details>
  <div class="toolbar"><span class="meta">${escHtml(meta.join(" · "))}</span>${actions}</div>
</div>
<div class="content">${body}</div>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  function post(command) { vscode.postMessage({ command }); }
  document.addEventListener('click', (e) => {
    const btn = e.target instanceof Element ? e.target.closest('button[data-command]') : null;
    if (btn) {
      post(btn.dataset.command);
    }
  });
</script>
</body>
</html>`;
  }
}

export function isReadOnly(sql: string): boolean {
  const stripped = sql
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^\s*(--|#).*$/gm, " ")
    .trim();
  const keyword = (stripped.match(/^\(?\s*([A-Za-z]+)/)?.[1] || "").toUpperCase();
  return READ_ONLY_KEYWORDS.has(keyword);
}

function buildResultHtml(result: DevLakeQueryResult | undefined): string {
  if (!result || result.columns.length === 0) {
    return '<div class="empty">Statement executed. No result set returned.</div>';
  }
  if (result.rows.length === 0) {
    return '<div class="empty">Query returned no rows.</div>';
  }

  const head = result.columns.map((c) => `<th>${escHtml(c)}</th>`).join("");
  const rows = result.rows
    .slice(0, MAX_RENDERED_ROWS)
    .map((row) => {
      const cells = result.columns
        .map((c) => {
          const v = row[c];
          if (v === null || v === undefined) {
            return '<td class="null">NULL</td>';
          }
          const text = formatCell(v);
          return `<td title="${escHtml(text)}">${escHtml(text)}</td>`;
        })
        .join("");
      return `<tr>${cells}</tr>`;
    })
    .join("\n");

  const note =
    result.rows.length > MAX_RENDERED_ROWS
      ? `<div class="note">Showing the first ${MAX_RENDERED_ROWS} of ${result.rows.length} rows. Use "Open as JSON" or "Open as CSV" for the full result.</div>`
      : "";

  return `<table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table>${note}`;
}

function formatCell(v: unknown): string {
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

function toCsv(result: DevLakeQueryResult): string {
  const esc = (v: unknown): string => {
    if (v === null || v === undefined) {
      return "";
    }
    const s = formatCell(v);
    return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  const lines = [result.columns.map(esc).join(",")];
  for (const row of result.rows) {
    lines.push(result.columns.map((c) => esc(row[c])).join(","));
  }
  return lines.join("\n") + "\n";
}

function firstLine(sql: string): string {
  const line = sql.split("\n").find((l) => l.trim()) || "";
  return line.length > 120 ? line.slice(0, 117) + "..." : line;
}
