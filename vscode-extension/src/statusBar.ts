import * as vscode from "vscode";

export class WorkflowStatusBar {
  private syncItem: vscode.StatusBarItem;
  private nameItem: vscode.StatusBarItem;

  constructor(workflowName: string, outputChannel: vscode.OutputChannel) {
    this.nameItem = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Left,
      100
    );
    this.nameItem.text = `$(symbol-method) ${workflowName}`;
    this.nameItem.tooltip = "LFM Workflow";
    this.nameItem.show();

    this.syncItem = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Left,
      99
    );
    this.syncItem.command = "lfm.showOutput";
    this.syncItem.show();

    this.setSaved();
  }

  setSaving(): void {
    this.syncItem.text = "$(sync~spin) Saving...";
    this.syncItem.tooltip = "Saving draft to server...";
    this.syncItem.color = undefined;
  }

  setSaved(): void {
    this.syncItem.text = "$(cloud-upload) Saved";
    this.syncItem.tooltip = "All changes saved to server";
    this.syncItem.color = undefined;
  }

  setError(message: string): void {
    this.syncItem.text = "$(error) Save Error";
    this.syncItem.tooltip = message;
    this.syncItem.color = new vscode.ThemeColor(
      "statusBarItem.errorForeground"
    );
  }

  dispose(): void {
    this.syncItem.dispose();
    this.nameItem.dispose();
  }
}
