import * as crypto from "crypto";
import * as vscode from "vscode";

/** Escapes a value for use in HTML text and quoted attribute values. */
export function escHtml(value: unknown): string {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** Returns a fresh nonce for a single webview render. */
export function createNonce(): string {
  return crypto.randomBytes(16).toString("base64");
}

/** Returns the Content-Security-Policy meta tag for a webview render. */
export function cspMeta(webview: vscode.Webview, nonce: string): string {
  const src = webview.cspSource;
  return (
    `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; ` +
    `style-src ${src} 'unsafe-inline'; script-src 'nonce-${nonce}'; ` +
    `img-src ${src} data:; font-src ${src};">`
  );
}

/** Webview options shared by all panels: scripts on, no local resources. */
export const WEBVIEW_OPTIONS: vscode.WebviewPanelOptions & vscode.WebviewOptions = {
  enableScripts: true,
  retainContextWhenHidden: true,
  localResourceRoots: [],
};

/** Returns true if a webview message is an object with a string `command`. */
export function isCommandMessage(
  msg: unknown
): msg is { command: string } & Record<string, unknown> {
  return (
    typeof msg === "object" &&
    msg !== null &&
    typeof (msg as { command?: unknown }).command === "string"
  );
}
