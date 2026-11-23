import * as vscode from "vscode";
import * as fs from "fs";
import * as path from "path";
import { ApiClient } from "./api";
import { isWorkflowFile, workflowFiles } from "./fileSystemProvider";

const STAGING_SCHEME = "lfm-staging";

type ChangeKind = "M" | "A" | "D";

const CHANGE_INFO: Record<ChangeKind, { tooltip: string; color: string; icon: string }> = {
  M: { tooltip: "Modified", color: "gitDecoration.modifiedResourceForeground", icon: "diff-modified" },
  A: { tooltip: "Added", color: "gitDecoration.addedResourceForeground", icon: "diff-added" },
  D: { tooltip: "Deleted", color: "gitDecoration.deletedResourceForeground", icon: "diff-removed" },
};

// Shows the working copy of a workflow against its latest published (staging)
// version in the Source Control view, with diffs and gutter change markers.
export class StagingScm implements vscode.Disposable {
  readonly sourceControl: vscode.SourceControl;
  private readonly group: vscode.SourceControlResourceGroup;
  private readonly rootUri: vscode.Uri;
  private readonly disposables: vscode.Disposable[] = [];
  private readonly contentChanged = new vscode.EventEmitter<vscode.Uri>();
  private readonly decorationsChanged = new vscode.EventEmitter<vscode.Uri[]>();

  private staging = new Map<string, string>();
  private loadedVersion: number | undefined;
  private changes = new Map<string, ChangeKind>();
  private refreshTimer: ReturnType<typeof setTimeout> | undefined;
  private refreshing: Promise<void> = Promise.resolve();

  constructor(
    private readonly api: ApiClient,
    private readonly workflowId: number,
    rootDir: string,
    private stagingVersion: number
  ) {
    this.rootUri = vscode.Uri.file(rootDir);

    this.sourceControl = vscode.scm.createSourceControl("lfm", "LFM Workflow", this.rootUri);
    this.sourceControl.inputBox.placeholder = "Publish note (Ctrl+Enter to publish to staging)";
    this.sourceControl.acceptInputCommand = {
      command: "lfm.publishFromScm",
      title: "Publish to Staging",
    };
    this.sourceControl.quickDiffProvider = {
      provideOriginalResource: (uri) => {
        const relative = this.relativePath(uri);
        return relative ? this.stagingUri(relative) : undefined;
      },
    };
    this.group = this.sourceControl.createResourceGroup("changes", "Changes");
    this.group.hideWhenEmpty = false;

    const watcher = vscode.workspace.createFileSystemWatcher(
      new vscode.RelativePattern(rootDir, "{input.js,output.js,test_input.json,sql/**,tests/**}")
    );
    watcher.onDidChange(() => this.scheduleRefresh());
    watcher.onDidCreate(() => this.scheduleRefresh());
    watcher.onDidDelete(() => this.scheduleRefresh());

    this.disposables.push(
      this.sourceControl,
      watcher,
      this.contentChanged,
      this.decorationsChanged,
      vscode.workspace.registerTextDocumentContentProvider(STAGING_SCHEME, {
        onDidChange: this.contentChanged.event,
        provideTextDocumentContent: (uri) => this.staging.get(uri.path.replace(/^\//, "")) ?? "",
      }),
      vscode.window.registerFileDecorationProvider({
        onDidChangeFileDecorations: this.decorationsChanged.event,
        provideFileDecoration: (uri) => {
          const kind = uri.scheme === "file" ? this.changes.get(this.relativePath(uri) ?? "") : undefined;
          if (!kind) {
            return undefined;
          }
          const info = CHANGE_INFO[kind];
          return new vscode.FileDecoration(kind, `${info.tooltip} since staging`, new vscode.ThemeColor(info.color));
        },
      })
    );

    void this.refresh();
  }

  // Call after publishing, when the staging version is known to have changed.
  setStagingVersion(version: number): void {
    this.stagingVersion = version;
    void this.refresh();
  }

  // Re-reads the staging version from the server, in case it was published elsewhere.
  async refreshFromServer(): Promise<void> {
    try {
      const wf = await this.api.getWorkflow(this.workflowId);
      this.stagingVersion = wf.version;
      this.loadedVersion = undefined;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      vscode.window.showErrorMessage(`Failed to refresh staging version: ${msg}`);
    }
    await this.refresh();
  }

  openChange(uri: vscode.Uri): Thenable<unknown> {
    const relative = this.relativePath(uri);
    if (!relative) {
      return vscode.commands.executeCommand("vscode.open", uri);
    }
    const stagingUri = this.stagingUri(relative);
    const kind = this.changes.get(relative);
    if (kind === "D") {
      return vscode.commands.executeCommand("vscode.open", stagingUri);
    }
    const left = this.stagingVersion > 0 ? `Staging v${this.stagingVersion}` : "Unpublished";
    return vscode.commands.executeCommand(
      "vscode.diff",
      stagingUri,
      uri,
      `${relative} (${left} ↔ Working)`
    );
  }

  private scheduleRefresh(): void {
    if (this.refreshTimer) {
      clearTimeout(this.refreshTimer);
    }
    this.refreshTimer = setTimeout(() => {
      this.refreshTimer = undefined;
      void this.refresh();
    }, 300);
  }

  private refresh(): Promise<void> {
    this.refreshing = this.refreshing.then(() => this.doRefresh());
    return this.refreshing;
  }

  private async doRefresh(): Promise<void> {
    const version = this.stagingVersion;
    if (version !== this.loadedVersion) {
      try {
        this.staging = version > 0
          ? workflowFiles(await this.api.getVersion(this.workflowId, version))
          : new Map();
        this.loadedVersion = version;
        for (const relative of this.staging.keys()) {
          this.contentChanged.fire(this.stagingUri(relative));
        }
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        this.group.label = `Changes (failed to load staging v${version}: ${msg})`;
        return;
      }
    }

    const working = this.readWorkingFiles();
    const changes = new Map<string, ChangeKind>();
    for (const [relative, content] of working) {
      const original = this.staging.get(relative);
      if (original === undefined) {
        changes.set(relative, "A");
      } else if (original !== content) {
        changes.set(relative, "M");
      }
    }
    for (const relative of this.staging.keys()) {
      if (!working.has(relative)) {
        changes.set(relative, "D");
      }
    }

    const affected = new Set([...this.changes.keys(), ...changes.keys()]);
    this.changes = changes;

    this.group.label = version > 0 ? `Changes vs staging v${version}` : "Changes (not yet published)";
    this.group.resourceStates = [...changes.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([relative, kind]) => {
        const uri = vscode.Uri.joinPath(this.rootUri, ...relative.split("/"));
        const info = CHANGE_INFO[kind];
        return {
          resourceUri: uri,
          command: { command: "lfm.scm.openChange", title: "Open Changes", arguments: [uri] },
          decorations: {
            tooltip: info.tooltip,
            strikeThrough: kind === "D",
            iconPath: new vscode.ThemeIcon(info.icon, new vscode.ThemeColor(info.color)),
          },
        };
      });
    this.sourceControl.count = changes.size;
    this.decorationsChanged.fire(
      [...affected].map((relative) => vscode.Uri.joinPath(this.rootUri, ...relative.split("/")))
    );
  }

  private readWorkingFiles(): Map<string, string> {
    const files = new Map<string, string>();
    const root = this.rootUri.fsPath;
    const visit = (relative: string): void => {
      const full = path.join(root, ...relative.split("/"));
      let stat: fs.Stats;
      try {
        stat = fs.statSync(full);
      } catch {
        return;
      }
      if (stat.isDirectory()) {
        for (const entry of fs.readdirSync(full)) {
          visit(`${relative}/${entry}`);
        }
      } else if (isWorkflowFile(relative)) {
        files.set(relative, fs.readFileSync(full, "utf-8"));
      }
    };
    for (const entry of ["input.js", "output.js", "test_input.json", "sql", "tests"]) {
      visit(entry);
    }
    return files;
  }

  private relativePath(uri: vscode.Uri): string | undefined {
    if (uri.scheme !== "file") {
      return undefined;
    }
    const relative = path.relative(this.rootUri.fsPath, uri.fsPath).replace(/\\/g, "/");
    return isWorkflowFile(relative) ? relative : undefined;
  }

  private stagingUri(relative: string): vscode.Uri {
    return vscode.Uri.from({ scheme: STAGING_SCHEME, path: `/${relative}` });
  }

  dispose(): void {
    if (this.refreshTimer) {
      clearTimeout(this.refreshTimer);
    }
    this.disposables.forEach((d) => d.dispose());
  }
}
