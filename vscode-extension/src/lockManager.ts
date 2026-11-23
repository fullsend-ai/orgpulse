import * as crypto from "crypto";
import { ApiClient } from "./api";

export class LockManager {
  readonly sessionId: string;
  private locks = new Map<number, ReturnType<typeof setInterval>>();
  private static readonly HEARTBEAT_MS = 10_000;

  constructor(private api: ApiClient) {
    this.sessionId = `vscode-${crypto.randomUUID()}`;
  }

  async acquire(workflowId: number): Promise<boolean> {
    if (this.locks.has(workflowId)) {
      return true;
    }

    try {
      await this.api.lock(workflowId, this.sessionId);
    } catch {
      return false;
    }

    const timer = setInterval(() => {
      this.api.lock(workflowId, this.sessionId).catch(() => {});
    }, LockManager.HEARTBEAT_MS);

    this.locks.set(workflowId, timer);
    return true;
  }

  async release(workflowId: number): Promise<void> {
    const timer = this.locks.get(workflowId);
    if (timer) {
      clearInterval(timer);
      this.locks.delete(workflowId);
    }
    try {
      await this.api.unlock(workflowId, this.sessionId);
    } catch {
      // best-effort
    }
  }

  isLockedByUs(workflowId: number): boolean {
    return this.locks.has(workflowId);
  }

  async releaseAll(): Promise<void> {
    const ids = Array.from(this.locks.keys());
    for (const id of ids) {
      await this.release(id);
    }
  }

  dispose(): void {
    for (const timer of this.locks.values()) {
      clearInterval(timer);
    }
    this.locks.clear();
  }
}
