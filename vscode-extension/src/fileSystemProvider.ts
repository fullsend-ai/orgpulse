import * as vscode from "vscode";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";
import { ApiClient, Workflow, WorkflowQuery, WorkflowTestCase } from "./api";

export interface WorkflowMarker {
  id: number;
  name: string;
}

export function readMarkerFile(
  workspaceRoot: string
): WorkflowMarker | undefined {
  const markerPath = path.join(workspaceRoot, ".lfm-workflow.json");
  try {
    const raw = fs.readFileSync(markerPath, "utf-8");
    const data = JSON.parse(raw);
    if (typeof data.id === "number" && typeof data.name === "string") {
      return { id: data.id, name: data.name };
    }
  } catch {
    // not a workflow directory
  }
  return undefined;
}

export class WorkflowDiskProvider {
  private baseDir: string;
  private workflows = new Map<number, Workflow>();
  private watchers = new Map<number, vscode.Disposable[]>();
  private saveTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private lastWritten = new Map<string, string>();
  private docsLoaded = false;

  onSaveStart: (() => void) | undefined;
  onSaveEnd: ((error?: string) => void) | undefined;
  onWarning: ((message: string) => void) | undefined;

  constructor(private api: ApiClient) {
    this.baseDir = path.join(os.tmpdir(), "lfm-workflows");
    fs.mkdirSync(this.baseDir, { recursive: true });
  }

  getBaseDir(): string {
    return this.baseDir;
  }

  workflowDir(id: number, name: string): string {
    return path.join(this.baseDir, `${id}-${safeName(name)}`);
  }

  async loadWorkflow(id: number): Promise<Workflow> {
    const wf = await this.api.getWorkflow(id);
    this.workflows.set(id, wf);
    this.writeFiles(wf);
    this.writeMarkerFile(wf);
    this.writeContextFile(wf);
    this.watchWorkflow(wf);

    if (!this.docsLoaded) {
      this.docsLoaded = true;
      this.loadDocs(id).catch((err: Error) => {
        vscode.window.showWarningMessage(
          `Failed to load reference docs: ${err.message}`
        );
      });
    }

    return wf;
  }

  writeMarkerFile(wf: Workflow): void {
    const dir = this.workflowDir(wf.id, wf.name);
    fs.writeFileSync(
      path.join(dir, ".lfm-workflow.json"),
      JSON.stringify({ id: wf.id, name: wf.name }, null, 2),
      "utf-8"
    );
    const vscodeDir = path.join(dir, ".vscode");
    fs.mkdirSync(vscodeDir, { recursive: true });
    fs.writeFileSync(
      path.join(vscodeDir, "settings.json"),
      JSON.stringify(
        { "files.exclude": { ".lfm-workflow.json": true } },
        null,
        2
      ),
      "utf-8"
    );
  }

  async loadDocs(workflowId?: number): Promise<void> {
    let docs: string;
    if (workflowId !== undefined) {
      const wf = this.workflows.get(workflowId);
      if (wf) {
        docs = path.join(this.workflowDir(wf.id, wf.name), "docs");
      } else {
        docs = path.join(this.baseDir, "docs");
      }
    } else {
      docs = path.join(this.baseDir, "docs");
    }
    fs.mkdirSync(docs, { recursive: true });

    const dbSchemaPromise = Promise.race([
      this.fetchDevLakeSchema(docs),
      new Promise<string>((_, reject) =>
        setTimeout(() => reject(new Error("timed out")), 15_000)
      ),
    ]).catch((err) => {
      this.warn(`Failed to fetch DevLake schema: ${err}`);
      return "";
    });

    const [schemas, procs, scripts, queries, dbSchema] =
      await Promise.all([
        this.fetchSchemas(docs),
        this.fetchStoredProcedures(docs, workflowId),
        this.fetchCannedScripts(docs),
        this.fetchCannedQueries(docs),
        dbSchemaPromise,
      ]);

    this.warn(
      `Docs loaded: ${schemas.length} schemas, ${procs.length} stored procedures, ` +
      `${scripts.length} scripts, ${queries.length} queries, ` +
      `db schema ${dbSchema ? "yes" : "no"}`
    );

    const guide = buildWorkflowGuide(schemas, procs, scripts, queries, dbSchema);
    fs.writeFileSync(path.join(docs, "WORKFLOW_GUIDE.md"), guide, "utf-8");
  }

  private async fetchSchemas(
    docs: string
  ): Promise<{ name: string; description?: string }[]> {
    const dir = path.join(docs, "schemas");
    fs.mkdirSync(dir, { recursive: true });
    try {
      const items = await this.api.listJsonSchemas() || [];
      const summaries: { name: string; description?: string }[] = [];
      for (const s of items) {
        const fileName = `${safeName(s.name)}.json`;
        let content = s.json_schema;
        try {
          content = JSON.stringify(JSON.parse(s.json_schema), null, 2);
        } catch {
          // keep raw
        }
        const header =
          `// Schema: ${s.name}` +
          (s.description ? `\n// ${s.description}` : "") +
          `\n// ID: ${s.id}\n`;
        fs.writeFileSync(path.join(dir, fileName), header + content, "utf-8");
        summaries.push({ name: s.name, description: s.description });
      }
      return summaries;
    } catch (err) {
      this.warn(`Failed to fetch JSON schemas: ${err}`);
      return [];
    }
  }

  private async fetchStoredProcedures(
    docs: string,
    workflowId?: number
  ): Promise<{ name: string; description?: string }[]> {
    const dir = path.join(docs, "stored-procedures");
    fs.mkdirSync(dir, { recursive: true });
    try {
      const items = await this.api.listStoredProcedures(workflowId) || [];
      const summaries: { name: string; description?: string }[] = [];
      for (const p of items) {
        const fileName = `${safeName(p.name)}.sql`;
        const header =
          `-- Stored Procedure: ${p.name}` +
          (p.description ? `\n-- ${p.description}` : "") +
          (p.workflow_id ? `\n-- Deployed from workflow ID: ${p.workflow_id}` : "") +
          `\n-- ID: ${p.id}\n\n`;
        fs.writeFileSync(
          path.join(dir, fileName),
          header + (p.sql_text || ""),
          "utf-8"
        );
        summaries.push({ name: p.name, description: p.description });
      }
      return summaries;
    } catch (err) {
      this.warn(`Failed to fetch stored procedures: ${err}`);
      return [];
    }
  }

  private async fetchCannedScripts(
    docs: string
  ): Promise<{ name: string; category: string; description?: string }[]> {
    const dir = path.join(docs, "scripts");
    fs.mkdirSync(dir, { recursive: true });
    try {
      const items = await this.api.listCannedScripts() || [];
      const summaries: {
        name: string;
        category: string;
        description?: string;
      }[] = [];
      for (const s of items) {
        const fileName = `${safeName(s.name)}.js`;
        const header =
          `// Script: ${s.name}` +
          (s.description ? `\n// ${s.description}` : "") +
          `\n// Category: ${s.category}` +
          `\n// ID: ${s.id}\n\n`;
        fs.writeFileSync(
          path.join(dir, fileName),
          header + (s.js_text || ""),
          "utf-8"
        );
        summaries.push({
          name: s.name,
          category: s.category,
          description: s.description,
        });
      }
      return summaries;
    } catch (err) {
      this.warn(`Failed to fetch canned scripts: ${err}`);
      return [];
    }
  }

  private async fetchCannedQueries(
    docs: string
  ): Promise<{ name: string; category: string; description?: string }[]> {
    const dir = path.join(docs, "queries");
    fs.mkdirSync(dir, { recursive: true });
    try {
      const items = await this.api.listCannedQueries() || [];
      const summaries: {
        name: string;
        category: string;
        description?: string;
      }[] = [];
      for (const q of items) {
        const fileName = `${safeName(q.name)}.sql`;
        let header =
          `-- Query: ${q.name}` +
          (q.description ? `\n-- ${q.description}` : "") +
          `\n-- Category: ${q.category}` +
          `\n-- ID: ${q.id}`;
        if (q.parameters) {
          header += `\n-- Parameters: ${JSON.stringify(q.parameters)}`;
        }
        header += "\n\n";
        fs.writeFileSync(
          path.join(dir, fileName),
          header + (q.sql_text || ""),
          "utf-8"
        );
        summaries.push({
          name: q.name,
          category: q.category,
          description: q.description,
        });
      }
      return summaries;
    } catch (err) {
      this.warn(`Failed to fetch canned queries: ${err}`);
      return [];
    }
  }

  private async fetchDevLakeSchema(docs: string): Promise<string> {
    try {
      const result = await this.api.devlakeQuery(
        `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT
         FROM information_schema.COLUMNS
         WHERE TABLE_SCHEMA = DATABASE()
         ORDER BY TABLE_NAME, ORDINAL_POSITION`
      );
      const rows = (result.rows || []) as Record<string, unknown>[];
      if (rows.length === 0) {
        return "";
      }

      const lines: string[] = [
        "# DevLake Database Schema",
        "",
        "Domain-layer tables available for workflow SQL queries.",
        "",
      ];

      let currentTable = "";
      for (const row of rows) {
        const table = String(row["TABLE_NAME"] || "");
        if (table !== currentTable) {
          if (currentTable) {
            lines.push("");
          }
          currentTable = table;
          lines.push(`## ${table}`);
          lines.push("");
          lines.push("| Column | Type | Null | Key | Default |");
          lines.push("|--------|------|------|-----|---------|");
        }
        const col = row["COLUMN_NAME"] || "";
        const type = row["COLUMN_TYPE"] || "";
        const nullable = row["IS_NULLABLE"] || "";
        const key = row["COLUMN_KEY"] || "";
        const def = row["COLUMN_DEFAULT"] ?? "";
        lines.push(`| ${col} | ${type} | ${nullable} | ${key} | ${def} |`);
      }
      lines.push("");

      const content = lines.join("\n");
      fs.writeFileSync(
        path.join(docs, "devlake-db-schema.md"),
        content,
        "utf-8"
      );
      return content;
    } catch (err) {
      this.warn(`Failed to fetch DevLake schema: ${err}`);
      return "";
    }
  }

  writeContextFile(wf: Workflow): void {
    const dir = this.workflowDir(wf.id, wf.name);
    const queries = wf.queries || [];
    const tests = wf.tests || [];

    const lines: string[] = [
      `# Workflow: ${wf.name}`,
      "",
      "You are editing an LFM data-transformation workflow.",
      "**Only modify files in this directory.** Do not touch files outside it.",
      "",
      "## File Structure",
      "",
      "| File | Purpose |",
      "|---|---|",
      "| `input.js` | Transform the HTTP request body into named SQL parameters (ES5.1, goja runtime) |",
      "| `output.js` | Transform query results into the final HTTP response (ES5.1, goja runtime) |",
      "| `test_input.json` | Default test input for manual runs |",
      "| `sql/step-N/*.sql` | SQL queries grouped into sequential stages. Queries within a stage run in parallel |",
      "| `tests/*/input.json` | Test case input |",
      "| `tests/*/assert.js` | Test assertion script (receives `output` and `error` globals) |",
      "",
      "## JavaScript Runtime Constraints",
      "",
      "Scripts run in a **goja ES5.1 sandbox**. You must NOT use:",
      "- `let`, `const`, arrow functions, template literals, destructuring, `async`/`await`",
      "- `require`, `import`, `fetch`, `setTimeout`, or any Node/browser API",
      "",
      "Use `var` and `function`. The last expression is the return value (no `return` statement).",
      "Wrap object literals in parentheses: `({ key: value })`.",
      "`JSON.parse()`, `JSON.stringify()`, and `console.log/info/warn/error` are available.",
      "",
      "## SQL Parameters",
      "",
      "Use `:paramName` in SQL to reference values from `input.js` output.",
      "Arrays expand to `?, ?, ?` for `IN` clauses. Parameters inside string literals and comments are ignored.",
      "Results from earlier stages are available as `:queryLabel`.",
      "",
    ];

    if (queries.length > 0) {
      lines.push("## Current Queries");
      lines.push("");
      lines.push("| Stage | Label | File |");
      lines.push("|---|---|---|");
      for (const q of queries) {
        lines.push(
          `| step-${q.stage ?? 0} | ${q.label} | \`sql/step-${q.stage ?? 0}/${safeName(q.label)}.sql\` |`
        );
      }
      lines.push("");
    }

    if (tests.length > 0) {
      lines.push("## Test Cases");
      lines.push("");
      for (const tc of tests) {
        lines.push(`- \`tests/${safeName(tc.name)}/\` — ${tc.name}`);
      }
      lines.push("");
    }

    lines.push("## Reference Documentation");
    lines.push("");
    lines.push("The `docs` directory contains:");
    lines.push("- `WORKFLOW_GUIDE.md` — full development guide");
    lines.push("- `devlake-db-schema.md` — database table/column reference");
    lines.push("- `schemas/` — JSON schemas for output validation");
    lines.push("- `stored-procedures/` — reusable SQL procedures");
    lines.push("- `queries/` — reusable SQL query patterns");
    lines.push("- `scripts/` — reusable JS script patterns");
    lines.push("");

    const content = lines.join("\n");
    fs.writeFileSync(path.join(dir, "AGENTS.md"), content, "utf-8");
    fs.writeFileSync(path.join(dir, "CLAUDE.md"), content, "utf-8");
  }

  private writeFiles(wf: Workflow): void {
    const dir = this.workflowDir(wf.id, wf.name);
    fs.mkdirSync(path.join(dir, "tests"), { recursive: true });
    for (const [relative, content] of workflowFiles(wf)) {
      const filePath = path.join(dir, ...relative.split("/"));
      fs.mkdirSync(path.dirname(filePath), { recursive: true });
      this.writeFileSafe(filePath, content);
    }
  }

  private writeTests(dir: string, tests: WorkflowTestCase[]): void {
    const testsDir = path.join(dir, "tests");
    fs.mkdirSync(testsDir, { recursive: true });

    for (const tc of tests) {
      const tcDir = path.join(testsDir, safeName(tc.name));
      fs.mkdirSync(tcDir, { recursive: true });
      for (const [fileName, content] of testCaseFiles(tc)) {
        this.writeFileSafe(path.join(tcDir, fileName), content);
      }
    }
  }

  addTest(workflowId: number, name: string): boolean {
    const wf = this.workflows.get(workflowId);
    if (!wf) {
      return false;
    }
    if (!wf.tests) {
      wf.tests = [];
    }
    if (wf.tests.find((t) => t.name === name)) {
      return false;
    }
    wf.tests.push({
      name,
      input: "{}",
      assert_script: "",
    });
    const dir = this.workflowDir(wf.id, wf.name);
    this.writeTests(dir, wf.tests);
    this.scheduleSave(wf);
    return true;
  }

  deleteTest(workflowId: number, testName: string): boolean {
    const wf = this.workflows.get(workflowId);
    if (!wf || !wf.tests) {
      return false;
    }
    const idx = wf.tests.findIndex((t) => t.name === testName);
    if (idx < 0) {
      return false;
    }
    wf.tests.splice(idx, 1);

    const dir = this.workflowDir(wf.id, wf.name);
    const tcDir = path.join(dir, "tests", safeName(testName));
    try {
      fs.rmSync(tcDir, { recursive: true, force: true });
    } catch {
      // ignore
    }

    this.scheduleSave(wf);
    return true;
  }

  private scheduleSave(wf: Workflow): void {
    const key = `__workflow_${wf.id}`;
    const existing = this.saveTimers.get(key);
    if (existing) {
      clearTimeout(existing);
    }
    if (this.onSaveStart) {
      this.onSaveStart();
    }
    this.saveTimers.set(
      key,
      setTimeout(() => {
        this.saveTimers.delete(key);
        this.saveToDraft(wf)
          .then(() => {
            if (this.onSaveEnd) {
              this.onSaveEnd();
            }
          })
          .catch((err) => {
            const msg = err instanceof Error ? err.message : String(err);
            if (this.onSaveEnd) {
              this.onSaveEnd(msg);
            }
            vscode.window.showErrorMessage(
              `Failed to save workflow: ${msg}`
            );
          });
      }, 500)
    );
  }

  private writeFileSafe(filePath: string, content: string): void {
    this.lastWritten.set(filePath, content);
    fs.writeFileSync(filePath, content, "utf-8");
  }

  private watchWorkflow(wf: Workflow): void {
    this.disposeWatchers(wf.id);

    const dir = this.workflowDir(wf.id, wf.name);
    const pattern = new vscode.RelativePattern(dir, "**/*.{js,sql,json}");
    const watcher = vscode.workspace.createFileSystemWatcher(pattern);

    const onChange = (uri: vscode.Uri) =>
      this.handleFileChange(uri, wf.id);
    watcher.onDidChange(onChange);
    watcher.onDidCreate((uri) => this.handleFileCreate(uri, wf.id));
    watcher.onDidDelete((uri) => this.handleFileDelete(uri, wf.id));

    const prefix = dir + path.sep;

    const onSave = vscode.workspace.onDidSaveTextDocument((doc) => {
      if (doc.uri.fsPath.startsWith(prefix)) {
        this.handleFileChange(doc.uri, wf.id);
      }
    });

    const onCreate = vscode.workspace.onDidCreateFiles((e) => {
      for (const uri of e.files) {
        if (uri.fsPath.startsWith(prefix)) {
          this.handleFileCreate(uri, wf.id);
        }
      }
    });

    const onDelete = vscode.workspace.onDidDeleteFiles((e) => {
      for (const uri of e.files) {
        if (uri.fsPath.startsWith(prefix)) {
          this.handleFileDelete(uri, wf.id);
        }
      }
    });

    const onRename = vscode.workspace.onDidRenameFiles((e) => {
      for (const { oldUri, newUri } of e.files) {
        if (oldUri.fsPath.startsWith(prefix)) {
          this.handleFileDelete(oldUri, wf.id);
        }
        if (newUri.fsPath.startsWith(prefix)) {
          this.handleFileCreate(newUri, wf.id);
        }
      }
    });

    this.watchers.set(wf.id, [watcher, onSave, onCreate, onDelete, onRename]);
  }

  private handleFileChange(uri: vscode.Uri, workflowId: number): void {
    const filePath = uri.fsPath;

    let content: string;
    try {
      content = fs.readFileSync(filePath, "utf-8");
    } catch {
      return;
    }

    if (this.lastWritten.get(filePath) === content) {
      return;
    }
    this.lastWritten.set(filePath, content);

    const wf = this.workflows.get(workflowId);
    if (!wf) {
      return;
    }

    const dir = this.workflowDir(wf.id, wf.name);
    const relative = path.relative(dir, filePath).replace(/\\/g, "/");

    let changed = false;

    if (relative === "input.js") {
      wf.input_script = content;
      changed = true;
    } else if (relative === "output.js") {
      wf.output_script = content;
      changed = true;
    } else if (relative === "test_input.json") {
      wf.test_input = content;
      changed = true;
    } else if (relative.startsWith("sql/")) {
      const match = relative.match(/^sql\/step-(\d+)\/(.+)\.sql$/);
      if (match) {
        const stage = parseInt(match[1]);
        const labelSlug = match[2];
        const query = (wf.queries || []).find(
          (q) =>
            (q.stage ?? 0) === stage &&
            (q.label === labelSlug || safeName(q.label) === labelSlug)
        );
        if (query) {
          query.sql_text = content;
          changed = true;
        }
      }
    } else if (relative.startsWith("tests/")) {
      const testMatch = relative.match(
        /^tests\/([^/]+)\/(input\.json|assert\.js|meta\.json)$/
      );
      if (testMatch) {
        const testSlug = testMatch[1];
        const fileName = testMatch[2];
        const tc = (wf.tests || []).find(
          (t) => safeName(t.name) === testSlug
        );
        if (tc) {
          if (fileName === "input.json") {
            tc.input = content;
            changed = true;
          } else if (fileName === "assert.js") {
            tc.assert_script = content;
            changed = true;
          } else if (fileName === "meta.json") {
            try {
              const meta = JSON.parse(content);
              if (meta.schema_id !== undefined) {
                tc.schema_id = meta.schema_id || undefined;
                changed = true;
              }
            } catch {
              // ignore invalid meta JSON
            }
          }
        }
      }
    }

    if (changed) {
      this.scheduleSave(wf);
    }
  }

  private handleFileCreate(uri: vscode.Uri, workflowId: number): void {
    const filePath = uri.fsPath;
    const wf = this.workflows.get(workflowId);
    if (!wf) {
      return;
    }

    let content: string;
    try {
      content = fs.readFileSync(filePath, "utf-8");
    } catch {
      return;
    }
    this.lastWritten.set(filePath, content);

    const dir = this.workflowDir(wf.id, wf.name);
    const relative = path.relative(dir, filePath).replace(/\\/g, "/");

    if (relative === "input.js") {
      wf.input_script = content;
    } else if (relative === "output.js") {
      wf.output_script = content;
    } else if (relative === "test_input.json") {
      wf.test_input = content;
    } else if (relative.startsWith("sql/")) {
      const match = relative.match(/^sql\/step-(\d+)\/(.+)\.sql$/);
      if (match) {
        const stage = parseInt(match[1]);
        const labelSlug = match[2];
        if (!wf.queries) {
          wf.queries = [];
        }
        const existing = wf.queries.find(
          (q) =>
            (q.stage ?? 0) === stage &&
            (q.label === labelSlug || safeName(q.label) === labelSlug)
        );
        if (existing) {
          existing.sql_text = content;
        } else {
          const stageQueries = wf.queries.filter(
            (q) => (q.stage ?? 0) === stage
          );
          wf.queries.push({
            label: labelSlug,
            stage: stage,
            sort_order: stageQueries.length,
            sql_text: content,
          });
        }
      }
    } else if (relative.startsWith("tests/")) {
      const testMatch = relative.match(
        /^tests\/([^/]+)\/(input\.json|assert\.js|meta\.json)$/
      );
      if (testMatch) {
        const testSlug = testMatch[1];
        const fileName = testMatch[2];
        if (!wf.tests) {
          wf.tests = [];
        }
        let tc = wf.tests.find((t) => safeName(t.name) === testSlug);
        if (!tc) {
          tc = { name: testSlug, input: "{}", assert_script: "" };
          wf.tests.push(tc);
        }
        if (fileName === "input.json") {
          tc.input = content;
        } else if (fileName === "assert.js") {
          tc.assert_script = content;
        } else if (fileName === "meta.json") {
          try {
            const meta = JSON.parse(content);
            if (meta.schema_id !== undefined) {
              tc.schema_id = meta.schema_id || undefined;
            }
          } catch {
            // ignore
          }
        }
      }
    } else {
      return;
    }

    this.scheduleSave(wf);
  }

  private handleFileDelete(uri: vscode.Uri, workflowId: number): void {
    const filePath = uri.fsPath;
    const wf = this.workflows.get(workflowId);
    if (!wf) {
      return;
    }

    this.lastWritten.delete(filePath);

    const dir = this.workflowDir(wf.id, wf.name);
    const relative = path.relative(dir, filePath).replace(/\\/g, "/");

    if (relative.startsWith("sql/")) {
      const match = relative.match(/^sql\/step-(\d+)\/(.+)\.sql$/);
      if (match && wf.queries) {
        const stage = parseInt(match[1]);
        const labelSlug = match[2];
        const idx = wf.queries.findIndex(
          (q) =>
            (q.stage ?? 0) === stage &&
            (q.label === labelSlug || safeName(q.label) === labelSlug)
        );
        if (idx >= 0) {
          wf.queries.splice(idx, 1);
        }
      }
    } else if (relative.startsWith("tests/")) {
      const testMatch = relative.match(/^tests\/([^/]+)\//);
      if (testMatch && wf.tests) {
        const testSlug = testMatch[1];
        const tcDir = path.join(dir, "tests", testSlug);
        if (!fs.existsSync(tcDir)) {
          const idx = wf.tests.findIndex(
            (t) => safeName(t.name) === testSlug
          );
          if (idx >= 0) {
            wf.tests.splice(idx, 1);
          }
        }
      }
    } else {
      return;
    }

    this.scheduleSave(wf);
  }

  private async saveToDraft(wf: Workflow): Promise<void> {
    await this.api.saveDraft(wf.id, {
      name: wf.name,
      description: wf.description,
      input_script: wf.input_script,
      output_script: wf.output_script,
      test_input: wf.test_input,
      version: wf.version,
      queries: wf.queries,
      tests: wf.tests,
    });
  }

  getWorkflow(id: number): Workflow | undefined {
    return this.workflows.get(id);
  }

  async flushSaves(id: number): Promise<void> {
    const key = `__workflow_${id}`;
    const timer = this.saveTimers.get(key);
    if (timer) {
      clearTimeout(timer);
      this.saveTimers.delete(key);
    }
    const wf = this.workflows.get(id);
    if (!wf) {
      return;
    }
    this.syncFromDisk(wf);
    await this.saveToDraft(wf);
  }

  private syncFromDisk(wf: Workflow): void {
    const dir = this.workflowDir(wf.id, wf.name);

    try {
      wf.input_script = fs.readFileSync(
        path.join(dir, "input.js"),
        "utf-8"
      );
    } catch {
      // keep existing
    }
    try {
      wf.output_script = fs.readFileSync(
        path.join(dir, "output.js"),
        "utf-8"
      );
    } catch {
      // keep existing
    }
    try {
      wf.test_input = fs.readFileSync(
        path.join(dir, "test_input.json"),
        "utf-8"
      );
    } catch {
      // keep existing
    }

    const sqlDir = path.join(dir, "sql");
    if (fs.existsSync(sqlDir)) {
      const queries: { label: string; stage: number; sort_order: number; sql_text: string }[] = [];
      for (const entry of fs.readdirSync(sqlDir)) {
        const stageMatch = entry.match(/^step-(\d+)$/);
        if (!stageMatch) {
          continue;
        }
        const stage = parseInt(stageMatch[1]);
        const stageDir = path.join(sqlDir, entry);
        try {
          if (!fs.statSync(stageDir).isDirectory()) {
            continue;
          }
        } catch {
          continue;
        }
        const files = fs.readdirSync(stageDir)
          .filter((f) => f.endsWith(".sql"))
          .sort();
        for (let i = 0; i < files.length; i++) {
          const label = files[i].replace(/\.sql$/, "");
          let content = "";
          try {
            content = fs.readFileSync(
              path.join(stageDir, files[i]),
              "utf-8"
            );
          } catch {
            continue;
          }
          const prev = (wf.queries || []).find(
            (q) =>
              (q.stage ?? 0) === stage &&
              (q.label === label || safeName(q.label) === label)
          );
          queries.push({
            label: label,
            stage: stage,
            sort_order: prev?.sort_order ?? i,
            sql_text: content,
          });
          this.lastWritten.set(
            path.join(stageDir, files[i]),
            content
          );
        }
      }
      wf.queries = queries;
    }

    const testsDir = path.join(dir, "tests");
    if (fs.existsSync(testsDir)) {
      const tests: { name: string; input: string; assert_script: string; schema_id?: number }[] = [];
      for (const entry of fs.readdirSync(testsDir)) {
        const tcDir = path.join(testsDir, entry);
        try {
          if (!fs.statSync(tcDir).isDirectory()) {
            continue;
          }
        } catch {
          continue;
        }
        const prev = (wf.tests || []).find(
          (t) => safeName(t.name) === entry
        );
        let input = "{}";
        let assertScript = "";
        let schemaId: number | undefined;
        try {
          input = fs.readFileSync(
            path.join(tcDir, "input.json"),
            "utf-8"
          );
        } catch {
          // keep default
        }
        try {
          assertScript = fs.readFileSync(
            path.join(tcDir, "assert.js"),
            "utf-8"
          );
        } catch {
          // keep default
        }
        try {
          const meta = JSON.parse(
            fs.readFileSync(path.join(tcDir, "meta.json"), "utf-8")
          );
          if (meta.schema_id !== undefined) {
            schemaId = meta.schema_id || undefined;
          }
        } catch {
          // no meta
        }
        tests.push({
          name: prev?.name ?? entry,
          input,
          assert_script: assertScript,
          schema_id: schemaId,
        });
      }
      wf.tests = tests;
    }
  }

  rewriteFiles(id: number): void {
    const wf = this.workflows.get(id);
    if (!wf) {
      return;
    }
    const dir = this.workflowDir(wf.id, wf.name);
    try {
      fs.rmSync(path.join(dir, "sql"), { recursive: true, force: true });
    } catch {
      // ignore
    }
    try {
      fs.rmSync(path.join(dir, "tests"), { recursive: true, force: true });
    } catch {
      // ignore
    }
    this.writeFiles(wf);
  }

  clearWorkflow(id: number): void {
    this.disposeWatchers(id);

    const wf = this.workflows.get(id);
    if (wf) {
      const dir = this.workflowDir(wf.id, wf.name);

      for (const key of Array.from(this.lastWritten.keys())) {
        if (key.startsWith(dir)) {
          this.lastWritten.delete(key);
        }
      }

      for (const key of Array.from(this.saveTimers.keys())) {
        if (key.startsWith(dir)) {
          clearTimeout(this.saveTimers.get(key)!);
          this.saveTimers.delete(key);
        }
      }

      try {
        fs.rmSync(dir, { recursive: true, force: true });
      } catch {
        // ignore cleanup errors
      }
    }

    this.workflows.delete(id);
  }

  private warn(message: string): void {
    if (this.onWarning) {
      this.onWarning(message);
    }
  }

  private disposeWatchers(id: number): void {
    const watchers = this.watchers.get(id);
    if (watchers) {
      watchers.forEach((w) => w.dispose());
      this.watchers.delete(id);
    }
  }

  dispose(): void {
    for (const timer of this.saveTimers.values()) {
      clearTimeout(timer);
    }
    this.saveTimers.clear();

    for (const id of Array.from(this.workflows.keys())) {
      this.clearWorkflow(id);
    }

    try {
      fs.rmdirSync(this.baseDir);
    } catch {
      // might not be empty
    }
  }
}

export interface WorkflowContent {
  input_script?: string;
  output_script?: string;
  test_input?: string;
  queries?: WorkflowQuery[];
  tests?: WorkflowTestCase[];
}

// Maps workflow content to its on-disk files (workspace-relative, "/"-separated).
// The staging diff uses the same mapping, so keep this the single source of truth.
export function workflowFiles(wf: WorkflowContent): Map<string, string> {
  const files = new Map<string, string>();
  files.set("input.js", wf.input_script || "");
  files.set("output.js", wf.output_script || "");
  files.set("test_input.json", prettyJson(wf.test_input));
  for (const q of wf.queries || []) {
    files.set(`sql/step-${q.stage ?? 0}/${safeName(q.label)}.sql`, q.sql_text || "");
  }
  for (const tc of wf.tests || []) {
    for (const [fileName, content] of testCaseFiles(tc)) {
      files.set(`tests/${safeName(tc.name)}/${fileName}`, content);
    }
  }
  return files;
}

export function isWorkflowFile(relative: string): boolean {
  return (
    relative === "input.js" ||
    relative === "output.js" ||
    relative === "test_input.json" ||
    /^sql\/step-\d+\/[^/]+\.sql$/.test(relative) ||
    /^tests\/[^/]+\/(input\.json|assert\.js|meta\.json)$/.test(relative)
  );
}

function testCaseFiles(tc: WorkflowTestCase): Array<[string, string]> {
  const files: Array<[string, string]> = [
    ["input.json", prettyJson(tc.input)],
    ["assert.js", tc.assert_script || ""],
  ];
  if (tc.schema_id) {
    files.push(["meta.json", JSON.stringify({ schema_id: tc.schema_id }, null, 2)]);
  }
  return files;
}

function prettyJson(raw: string | undefined): string {
  try {
    return JSON.stringify(JSON.parse(raw || "{}"), null, 2);
  } catch {
    return raw || "{}";
  }
}

function safeName(name: string): string {
  return name.replace(/[^a-zA-Z0-9_-]/g, "_");
}

function buildWorkflowGuide(
  schemas: { name: string; description?: string }[],
  procs: { name: string; description?: string }[],
  scripts: { name: string; category: string; description?: string }[],
  queries: { name: string; category: string; description?: string }[],
  _dbSchema: string
): string {
  const lines: string[] = [];

  lines.push("# LFM Workflow Development Guide");
  lines.push("");
  lines.push("This document describes how to create and edit workflows for the LFM Dashboard platform.");
  lines.push("Use this as a reference when writing input scripts, SQL queries, and output scripts.");
  lines.push("");

  // Architecture
  lines.push("## Workflow Architecture");
  lines.push("");
  lines.push("A workflow is a data transformation pipeline with three stages:");
  lines.push("");
  lines.push("```");
  lines.push("HTTP Request (JSON body)");
  lines.push("       │");
  lines.push("       ▼");
  lines.push("┌─────────────┐");
  lines.push("│  input.js   │  JavaScript: transform/validate the incoming request");
  lines.push("└──────┬──────┘");
  lines.push("       │ returns an object whose keys become named SQL parameters");
  lines.push("       ▼");
  lines.push("┌─────────────┐");
  lines.push("│ SQL Queries │  One or more SQL queries grouped into sequential stages");
  lines.push("│ (stages)    │  Queries within the same stage run in parallel");
  lines.push("└──────┬──────┘");
  lines.push("       │ each query's result rows are available by label");
  lines.push("       ▼");
  lines.push("┌─────────────┐");
  lines.push("│  output.js  │  JavaScript: transform all query results into the final response");
  lines.push("└──────┬──────┘");
  lines.push("       │");
  lines.push("       ▼");
  lines.push("  HTTP Response (JSON)");
  lines.push("```");
  lines.push("");

  // input.js
  lines.push("## input.js — Input Transformation");
  lines.push("");
  lines.push("The input script receives the HTTP request body as the global variable `input`.");
  lines.push("It must return an object. Each key of the returned object becomes a named parameter");
  lines.push("available in SQL queries via `:paramName` syntax.");
  lines.push("");
  lines.push("### Globals available");
  lines.push("");
  lines.push("| Variable | Type | Description |");
  lines.push("|----------|------|-------------|");
  lines.push("| `input`  | object | The parsed JSON body of the HTTP request |");
  lines.push("");
  lines.push("### Return value");
  lines.push("");
  lines.push("The last expression in the script is used as the return value (not `return`).");
  lines.push("It should be a plain object with keys that SQL queries will reference.");
  lines.push("");
  lines.push("### Example");
  lines.push("");
  lines.push("```javascript");
  lines.push("// input.js");
  lines.push("var teamId = input.team_id || 'default';");
  lines.push("var since = input.since || '2024-01-01';");
  lines.push("var repos = input.repos || [];");
  lines.push("");
  lines.push("({");
  lines.push("  teamId: teamId,");
  lines.push("  since: since,");
  lines.push("  repos: repos  // arrays expand to comma-separated values in SQL IN clauses");
  lines.push("})");
  lines.push("```");
  lines.push("");

  // SQL queries
  lines.push("## SQL Queries — Data Retrieval");
  lines.push("");
  lines.push("SQL queries run against the DevLake database (MySQL). They are organized into");
  lines.push("**stages** (step-0, step-1, etc.). Stages execute sequentially; queries within");
  lines.push("the same stage run in parallel.");
  lines.push("");
  lines.push("### Named parameters");
  lines.push("");
  lines.push("Use `:paramName` to reference values from the input script's output or from");
  lines.push("previous query results. The parameter resolver handles:");
  lines.push("");
  lines.push("- **Scalar values**: `:teamId` → replaced with a single `?` placeholder");
  lines.push("- **Arrays**: `:repos` → expanded to `?, ?, ?` (one per element), for use in `IN` clauses");
  lines.push("- **Previous query results**: the result rows of a query labeled `foo` are available as `:foo`");
  lines.push("");
  lines.push("Named parameters inside string literals (`'...'`), line comments (`--`),");
  lines.push("and block comments (`/* */`) are left untouched.");
  lines.push("");
  lines.push("### Query results");
  lines.push("");
  lines.push("Each query produces an array of row objects (`{column: value}`). The result");
  lines.push("is stored under the query's **label** and is available to:");
  lines.push("");
  lines.push("- Later-stage SQL queries as a named parameter");
  lines.push("- The output script via `results.labelName`");
  lines.push("");
  lines.push("### Example");
  lines.push("");
  lines.push("```sql");
  lines.push("-- sql/step-0/repo_commits.sql");
  lines.push("-- Label: repo_commits");
  lines.push("SELECT");
  lines.push("  r.name AS repo_name,");
  lines.push("  COUNT(*) AS commit_count");
  lines.push("FROM repos r");
  lines.push("JOIN commits c ON c.repo_id = r.id");
  lines.push("WHERE r.name IN (:repos)");
  lines.push("  AND c.authored_date >= :since");
  lines.push("GROUP BY r.name");
  lines.push("ORDER BY commit_count DESC");
  lines.push("```");
  lines.push("");

  // output.js
  lines.push("## output.js — Output Transformation");
  lines.push("");
  lines.push("The output script receives two global variables and must return the final JSON response.");
  lines.push("");
  lines.push("### Globals available");
  lines.push("");
  lines.push("| Variable  | Type | Description |");
  lines.push("|-----------|------|-------------|");
  lines.push("| `input`   | object | The object returned by `input.js` (the transformed input) |");
  lines.push("| `results` | object | Map of query label → array of row objects |");
  lines.push("");
  lines.push("### Example");
  lines.push("");
  lines.push("```javascript");
  lines.push("// output.js");
  lines.push("var commits = results.repo_commits || [];");
  lines.push("var total = 0;");
  lines.push("for (var i = 0; i < commits.length; i++) {");
  lines.push("  total += commits[i].commit_count;");
  lines.push("}");
  lines.push("");
  lines.push("({");
  lines.push("  team_id: input.teamId,");
  lines.push("  total_commits: total,");
  lines.push("  repos: commits");
  lines.push("})");
  lines.push("```");
  lines.push("");

  // JavaScript runtime
  lines.push("## JavaScript Runtime");
  lines.push("");
  lines.push("Scripts run in a Goja (ES5.1) sandbox. Key constraints:");
  lines.push("");
  lines.push("- **No ES6+**: no `let`/`const`, arrow functions, template literals,");
  lines.push("  destructuring, `async`/`await`, or modules. Use `var` and `function`.");
  lines.push("- **No Node/browser APIs**: no `require`, `fetch`, `setTimeout`, `Buffer`, etc.");
  lines.push("- **console**: `console.log()`, `.info()`, `.warn()`, `.error()`, `.debug()` are");
  lines.push("  available. Messages appear in the workflow execution logs.");
  lines.push("- **Return value**: the last expression in the script is the output. Do NOT use");
  lines.push("  `return`. Wrap objects in parentheses: `({ key: value })`.");
  lines.push("- **JSON**: `JSON.parse()` and `JSON.stringify()` are available.");
  lines.push("");

  // Testing
  lines.push("## Testing");
  lines.push("");
  lines.push("Workflows support test cases. Each test has:");
  lines.push("");
  lines.push("- **name**: descriptive test name");
  lines.push("- **input**: JSON string that simulates the HTTP request body");
  lines.push("- **assert_script** (optional): JS that receives `output` (the workflow result)");
  lines.push("  and `error` (string if pipeline failed). Throw to fail the test.");
  lines.push("- **schema_id** (optional): ID of a JSON schema to validate the output against");
  lines.push("");
  lines.push("### Assert script example");
  lines.push("");
  lines.push("```javascript");
  lines.push("if (!output || !output.repos) throw 'missing repos';");
  lines.push("if (output.repos.length === 0) throw 'expected at least one repo';");
  lines.push("```");
  lines.push("");

  // Stored procedures
  lines.push("## Stored Procedures");
  lines.push("");
  lines.push("Stored procedures are reusable SQL blocks deployed from workflows.");
  lines.push("They are called using `CALL procedure_name(args)` from SQL queries.");
  lines.push("See the `docs/stored-procedures/` folder for available procedures.");
  lines.push("");
  if (procs.length > 0) {
    lines.push("| Name | Description |");
    lines.push("|------|-------------|");
    for (const p of procs) {
      lines.push(`| ${p.name} | ${p.description || ""} |`);
    }
    lines.push("");
  }

  // JSON schemas
  lines.push("## JSON Schemas");
  lines.push("");
  lines.push("JSON schemas define the expected structure of workflow outputs.");
  lines.push("They can be referenced by test cases for automatic output validation.");
  lines.push("See the `docs/schemas/` folder for full schema definitions.");
  lines.push("");
  if (schemas.length > 0) {
    lines.push("| Name | Description |");
    lines.push("|------|-------------|");
    for (const s of schemas) {
      lines.push(`| ${s.name} | ${s.description || ""} |`);
    }
    lines.push("");
  }

  // Canned scripts
  lines.push("## Reusable JS Scripts");
  lines.push("");
  lines.push("Pre-built JavaScript fragments that can be used as patterns or copied");
  lines.push("into input/output scripts. See the `docs/scripts/` folder.");
  lines.push("");
  if (scripts.length > 0) {
    lines.push("| Name | Category | Description |");
    lines.push("|------|----------|-------------|");
    for (const s of scripts) {
      lines.push(`| ${s.name} | ${s.category} | ${s.description || ""} |`);
    }
    lines.push("");
  }

  // Canned queries
  lines.push("## Reusable SQL Queries");
  lines.push("");
  lines.push("Pre-built SQL queries that can be used as patterns or adapted for");
  lines.push("workflow SQL stages. See the `docs/queries/` folder.");
  lines.push("");
  if (queries.length > 0) {
    lines.push("| Name | Category | Description |");
    lines.push("|------|----------|-------------|");
    for (const q of queries) {
      lines.push(`| ${q.name} | ${q.category} | ${q.description || ""} |`);
    }
    lines.push("");
  }

  // DB schema reference
  lines.push("## DevLake Database Schema");
  lines.push("");
  lines.push("The DevLake database contains domain-layer tables with normalized software");
  lines.push("development data (commits, pull requests, issues, CI/CD, etc.).");
  lines.push("See `docs/devlake-db-schema.md` for the full table and column reference.");
  lines.push("");

  // File structure
  lines.push("## Workflow File Structure");
  lines.push("");
  lines.push("```");
  lines.push("<workflow-id>-<name>/");
  lines.push("├── input.js              # Input transformation script");
  lines.push("├── output.js             # Output transformation script");
  lines.push("└── sql/");
  lines.push("    ├── step-0/           # First stage (queries run in parallel)");
  lines.push("    │   ├── query_a.sql");
  lines.push("    │   └── query_b.sql");
  lines.push("    └── step-1/           # Second stage (runs after step-0 completes)");
  lines.push("        └── query_c.sql");
  lines.push("```");
  lines.push("");
  lines.push("Files are auto-saved to the server when you edit them.");
  lines.push("Changes are saved as a draft until explicitly published.");
  lines.push("");

  return lines.join("\n");
}
