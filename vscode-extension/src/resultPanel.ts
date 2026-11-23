import * as vscode from "vscode";
import {
  ApiClient,
  Workflow,
  RunStageRequest,
  StageResult,
  LogEntry,
} from "./api";
import {
  WEBVIEW_OPTIONS,
  createNonce,
  cspMeta,
  escHtml,
  isCommandMessage,
} from "./webviewUtils";

export class WorkflowResultPanel {
  private panel: vscode.WebviewPanel | undefined;
  private results: StageResult[] = [];
  private logs: LogEntry[] = [];
  private workflow: Workflow;

  constructor(
    private api: ApiClient,
    private extensionUri: vscode.Uri
  ) {
    this.workflow = {} as Workflow;
  }

  async run(wf: Workflow): Promise<void> {
    this.workflow = wf;

    let testInput: unknown;
    try {
      testInput = JSON.parse(wf.test_input || "{}");
    } catch {
      vscode.window.showErrorMessage("Invalid JSON in test_input.json");
      return;
    }

    this.createOrShowPanel(wf.name);
    this.updateHtml([], [], true);

    try {
      const result = await this.api.runWorkflow(wf.id, testInput);
      this.results = result.stages || [];
      this.logs = result.logs || [];
      this.updateHtml(this.results, this.logs, false);
      if (result.error) {
        vscode.window.showWarningMessage(
          `Workflow error: ${result.error}`
        );
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      this.results = [];
      this.logs = [];
      this.updateHtml([], [], false, msg);
      vscode.window.showErrorMessage(
        `Failed to run workflow: ${msg}`
      );
    }
  }

  private createOrShowPanel(title: string): void {
    if (this.panel) {
      this.panel.title = `Run: ${title}`;
      this.panel.reveal(vscode.ViewColumn.Beside);
      return;
    }

    this.panel = vscode.window.createWebviewPanel(
      "lfmWorkflowResult",
      `Run: ${title}`,
      vscode.ViewColumn.Beside,
      WEBVIEW_OPTIONS
    );

    this.panel.onDidDispose(() => {
      this.panel = undefined;
    });

    this.panel.webview.onDidReceiveMessage(
      (msg: unknown) => this.handleMessage(msg)
    );
  }

  private async handleMessage(msg: unknown): Promise<void> {
    if (!isCommandMessage(msg) || msg.command !== "rerun") {
      return;
    }
    const stageIndex = msg.stageIndex;
    if (
      typeof stageIndex !== "number" ||
      !Number.isInteger(stageIndex) ||
      typeof msg.stageName !== "string"
    ) {
      return;
    }
    // Ignore stale or forged messages that don't match the rendered stage.
    const stageName = this.results[stageIndex]?.stage;
    if (stageName === undefined || stageName !== msg.stageName) {
      return;
    }

    const wf = this.workflow;

    const inputStageOutput =
      this.results.find((r) => r.stage === "input_script")?.output ?? {};
    const queryResults: Record<string, unknown> = {};
    for (const r of this.results) {
      if (r.stage.startsWith("query:") && r.output) {
        const label = r.stage.replace(/^query:\d+:/, "");
        queryResults[label] = r.output;
      }
    }

    let req: RunStageRequest;

    if (stageName === "input_script") {
      let testInputVal: unknown;
      try {
        testInputVal = JSON.parse(wf.test_input || "{}");
      } catch {
        testInputVal = {};
      }
      req = {
        type: "js",
        script: wf.input_script || "",
        globals: { input: testInputVal },
      };
    } else if (stageName === "output_script") {
      req = {
        type: "js",
        script: wf.output_script || "",
        globals: { input: inputStageOutput, results: queryResults },
      };
    } else {
      const parts = stageName.split(":");
      const stepIdx = parseInt(parts[1] ?? "0") || 0;
      const label = parts.slice(2).join(":");
      const queries = wf.queries || [];
      const query = queries.find(
        (q) => (q.stage ?? 0) === stepIdx && q.label === label
      );
      const scope = Object.assign(
        {},
        queryResults,
        inputStageOutput as Record<string, unknown>
      );
      req = {
        type: "sql",
        script: query?.sql_text || "",
        scope,
      };
    }

    try {
      const resp = await this.api.runStage(req);
      this.results = this.results.map((r, i) =>
        i === stageIndex
          ? { ...r, output: resp.output, error: resp.error }
          : r
      );
      this.updateHtml(this.results, this.logs, false);
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : String(err);
      vscode.window.showErrorMessage(`Rerun failed: ${errMsg}`);
    }
  }

  private updateHtml(
    stages: StageResult[],
    logs: LogEntry[],
    running: boolean,
    error?: string
  ): void {
    if (!this.panel) {
      return;
    }
    this.panel.webview.html = this.buildHtml(
      this.panel.webview,
      stages,
      logs,
      running,
      error
    );
  }

  private buildHtml(
    webview: vscode.Webview,
    stages: StageResult[],
    logs: LogEntry[],
    running: boolean,
    error?: string
  ): string {
    const tabLabels: string[] = [];
    for (const r of stages) {
      if (r.stage === "input_script") {
        tabLabels.push("Input Transform");
      } else if (r.stage === "output_script") {
        tabLabels.push("Output Transform");
      } else {
        tabLabels.push(r.stage.replace("query:", "SQL: "));
      }
    }
    tabLabels.push("Console");

    const tabsHtml = tabLabels
      .map((label, i) => {
        const isConsole = i === tabLabels.length - 1;
        const isError = i < stages.length && stages[i]?.error;
        const activeClass = i === 0 ? " active" : "";
        const errorClass = isError ? " error" : "";
        const consoleStyle = isConsole ? ' style="margin-left:auto"' : "";
        const rerunBtn = !isConsole
          ? ` <span class="rerun" title="Re-run ${escHtml(label)}" data-rerun-idx="${i}" data-stage="${escHtml(stages[i]?.stage || "")}">&#8635;</span>`
          : "";
        return `<button class="tab${activeClass}${errorClass}" data-idx="${i}"${consoleStyle}>${escHtml(label)}${rerunBtn}</button>`;
      })
      .join("\n");

    const panelsHtml = stages
      .map((r, i) => {
        const display = i === 0 ? "block" : "none";
        const content = r.error
          ? `<pre class="error">${escHtml(r.error)}</pre>`
          : `<pre class="json">${highlightJson(jsonPretty(r.output))}</pre>`;
        const sqlSection = r.stage.startsWith("query:")
          ? `<details open><summary>Executed SQL</summary><pre class="sql">${escHtml(r.executedSql || "(No executed SQL returned by server)")}</pre></details>`
          : "";
        return `<div class="panel" data-idx="${i}" style="display:${display}">${content}${sqlSection}</div>`;
      })
      .join("\n");

    const consoleIdx = stages.length;
    const consoleDisplay = stages.length === 0 ? "block" : "none";
    const consoleLinesHtml =
      logs.length === 0
        ? '<div class="empty">No log output</div>'
        : logs
            .map(
              (l) =>
                `<div class="log ${escHtml(l.level)}">${l.stage ? `<span class="log-stage">[${escHtml(l.stage)}]</span> ` : ""}${escHtml(l.message)}</div>`
            )
            .join("\n");

    const bodyContent = running
      ? '<div class="running"><div class="spinner"></div>Running workflow...</div>'
      : error
        ? `<div class="error-banner">${escHtml(error)}</div>`
        : stages.length === 0 && !error
          ? '<div class="empty">No results yet.</div>'
          : `<div class="tabs">${tabsHtml}</div>
             <div class="panels">${panelsHtml}
               <div class="panel" data-idx="${consoleIdx}" style="display:${consoleDisplay}">${consoleLinesHtml}</div>
             </div>`;

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
    overflow: hidden;
    display: flex;
    flex-direction: column;
    height: 100vh;
  }
  .tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--vscode-panel-border, #333);
    overflow-x: auto;
    flex-shrink: 0;
  }
  .tab {
    padding: 6px 14px;
    border: none;
    background: transparent;
    color: var(--vscode-foreground);
    cursor: pointer;
    font-size: 12px;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .tab:hover { background: var(--vscode-list-hoverBackground, #2a2d2e); }
  .tab.active {
    border-bottom-color: var(--vscode-focusBorder, #007fd4);
    color: var(--vscode-foreground);
  }
  .tab.error { color: var(--vscode-errorForeground, #f14c4c); }
  .rerun {
    cursor: pointer;
    opacity: 0.6;
    font-size: 14px;
  }
  .rerun:hover { opacity: 1; }
  .panels {
    flex: 1;
    overflow: auto;
    padding: 8px 12px;
  }
  pre {
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--vscode-editor-font-family, monospace);
    font-size: var(--vscode-editor-font-size, 13px);
    line-height: 1.5;
  }
  pre.error { color: var(--vscode-errorForeground, #f14c4c); }
  pre.sql {
    color: var(--vscode-descriptionForeground, #888);
    margin-top: 4px;
  }
  details { margin-top: 8px; }
  summary {
    cursor: pointer;
    color: var(--vscode-textLink-foreground, #3794ff);
    font-size: 12px;
  }
  .empty {
    color: var(--vscode-descriptionForeground, #888);
    padding: 24px;
    text-align: center;
  }
  .error-banner {
    color: var(--vscode-errorForeground, #f14c4c);
    padding: 16px;
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
  .log { padding: 1px 0; font-family: var(--vscode-editor-font-family, monospace); font-size: 12px; }
  .json .j-key { color: var(--vscode-symbolIcon-propertyForeground, #9cdcfe); }
  .json .j-str { color: var(--vscode-debugTokenExpression-string, #ce9178); }
  .json .j-num { color: var(--vscode-debugTokenExpression-number, #b5cea8); }
  .json .j-bool { color: var(--vscode-debugTokenExpression-boolean, #569cd6); }
  .json .j-null { color: var(--vscode-debugTokenExpression-boolean, #569cd6); }
  .json .j-brace { color: var(--vscode-foreground); opacity: 0.7; }
  .log.warn { color: var(--vscode-editorWarning-foreground, #cca700); }
  .log.error { color: var(--vscode-errorForeground, #f14c4c); }
  .log-stage {
    color: var(--vscode-descriptionForeground, #888);
    margin-right: 6px;
  }
</style>
</head>
<body>
${bodyContent}
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  function switchTab(idx) {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.panel').forEach(p => p.style.display = 'none');
    const tab = document.querySelector('.tab[data-idx="'+idx+'"]');
    const panel = document.querySelector('.panel[data-idx="'+idx+'"]');
    if (tab) tab.classList.add('active');
    if (panel) panel.style.display = 'block';
  }
  function rerun(idx, stage) {
    vscode.postMessage({ command: 'rerun', stageIndex: idx, stageName: stage });
  }
  // Clicking the re-run icon also selects its tab, as the icon sits inside it.
  document.addEventListener('click', (e) => {
    const target = e.target instanceof Element ? e.target : null;
    if (!target) return;
    const rerunEl = target.closest('.rerun');
    if (rerunEl) {
      rerun(Number(rerunEl.dataset.rerunIdx), rerunEl.dataset.stage || '');
    }
    const tab = target.closest('.tab');
    if (tab) {
      switchTab(tab.dataset.idx);
    }
  });
</script>
</body>
</html>`;
  }
}

function highlightJson(json: string): string {
  return json.replace(
    /("(?:\\.|[^"\\])*")\s*(:)|("(?:\\.|[^"\\])*")|\b(true|false)\b|\b(null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b|([{}[\],])/g,
    (
      _match: string,
      key: string | undefined,
      colon: string | undefined,
      str: string | undefined,
      bool: string | undefined,
      nul: string | undefined,
      num: string | undefined,
      brace: string | undefined
    ) => {
      if (key && colon) {
        return `<span class="j-key">${escHtml(key)}</span>${colon}`;
      }
      if (str) {
        return `<span class="j-str">${escHtml(str)}</span>`;
      }
      if (bool) {
        return `<span class="j-bool">${bool}</span>`;
      }
      if (nul) {
        return `<span class="j-null">${nul}</span>`;
      }
      if (num) {
        return `<span class="j-num">${num}</span>`;
      }
      if (brace) {
        return `<span class="j-brace">${escHtml(brace)}</span>`;
      }
      return escHtml(_match);
    }
  );
}

function jsonPretty(val: unknown): string {
  try {
    return JSON.stringify(val, null, 2);
  } catch {
    return String(val);
  }
}
