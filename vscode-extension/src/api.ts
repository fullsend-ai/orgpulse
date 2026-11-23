import * as https from "https";
import * as http from "http";
import { URL } from "url";

export interface WorkflowQuery {
  id?: number;
  stage: number;
  sort_order: number;
  label: string;
  sql_text: string;
}

export interface WorkflowTestCase {
  name: string;
  input: string;
  assert_script: string;
  schema_id?: number;
}

export interface TestCaseResult {
  name: string;
  passed: boolean;
  error?: string;
}

export interface WorkflowListEntry {
  id: number;
  name: string;
  description?: string;
  input_script: string;
  output_script: string;
  test_input?: string;
  version: number;
  has_draft?: boolean;
  production_version?: number;
  archived?: boolean;
  queries?: WorkflowQuery[];
  tests?: WorkflowTestCase[];
  locked?: boolean;
  locked_session_id?: string;
}

export interface Workflow extends WorkflowListEntry {
  queries: WorkflowQuery[];
  tests: WorkflowTestCase[];
}

export interface WorkflowVersion {
  id: number;
  workflow_id: number;
  version: number;
  name: string;
  description?: string;
  input_script: string;
  output_script: string;
  test_input?: string;
  tests?: WorkflowTestCase[];
  queries?: WorkflowQuery[];
  publish_note?: string;
  production: boolean;
  published_at?: string;
}

export interface JsonSchema {
  id: number;
  name: string;
  description?: string;
  json_schema: string;
}

export interface StoredProcedure {
  id: number;
  workflow_id?: number;
  name: string;
  description?: string;
  sql_text: string;
}

export interface CannedScript {
  id: number;
  name: string;
  description?: string;
  category: string;
  js_text: string;
}

export interface CannedQuery {
  id: number;
  name: string;
  description?: string;
  category: string;
  sql_text: string;
  parameters?: unknown;
}

export interface DevLakeQueryResult {
  columns: string[];
  rows: Record<string, unknown>[];
}

export interface StageResult {
  stage: string;
  output?: unknown;
  error?: string;
  executedSql?: string;
}

export interface LogEntry {
  level: string;
  message: string;
  stage?: string;
}

export interface WorkflowResult {
  stages: StageResult[];
  output?: unknown;
  error?: string;
  logs: LogEntry[];
}

export interface RunStageRequest {
  type: "js" | "sql";
  script: string;
  globals?: Record<string, unknown>;
  params?: unknown[];
  scope?: Record<string, unknown>;
}

export class ApiClient {
  constructor(
    private baseUrl: string,
    private apiKey: string
  ) {}

  private request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    return new Promise((resolve, reject) => {
      const url = new URL(path, this.baseUrl);
      const mod = url.protocol === "https:" ? https : http;
      const payload = body !== undefined ? JSON.stringify(body) : undefined;

      const req = mod.request(
        url,
        {
          method,
          headers: {
            "X-API-Key": this.apiKey,
            "Content-Type": "application/json",
            ...(payload !== undefined
              ? { "Content-Length": Buffer.byteLength(payload).toString() }
              : {}),
          },
        },
        (res) => {
          let data = "";
          res.on("data", (chunk: string) => (data += chunk));
          res.on("end", () => {
            if (
              res.statusCode &&
              res.statusCode >= 200 &&
              res.statusCode < 300
            ) {
              try {
                resolve(JSON.parse(data) as T);
              } catch {
                resolve(data as unknown as T);
              }
            } else {
              let msg = `HTTP ${res.statusCode}`;
              try {
                const parsed = JSON.parse(data);
                if (parsed.error) {
                  msg = parsed.error;
                }
              } catch {
                // ignore
              }
              reject(new Error(msg));
            }
          });
        }
      );

      req.on("error", reject);
      if (payload !== undefined) {
        req.write(payload);
      }
      req.end();
    });
  }

  async listWorkflows(): Promise<WorkflowListEntry[]> {
    const all = await this.request<WorkflowListEntry[]>(
      "GET",
      "/api/v1/workflows"
    );
    return all.filter((w) => !w.archived);
  }

  getWorkflow(id: number): Promise<Workflow> {
    return this.request<Workflow>("GET", `/api/v1/workflows/${id}`);
  }

  createWorkflow(data: {
    name: string;
    description?: string;
    input_script: string;
    output_script: string;
    queries?: WorkflowQuery[];
  }): Promise<Workflow> {
    return this.request<Workflow>("POST", "/api/v1/workflows", data);
  }

  saveDraft(id: number, data: Partial<Workflow>): Promise<unknown> {
    return this.request("PUT", `/api/v1/workflows/${id}/draft`, data);
  }

  listVersions(id: number): Promise<WorkflowVersion[]> {
    return this.request<WorkflowVersion[]>(
      "GET",
      `/api/v1/workflows/${id}/versions`
    );
  }

  getVersion(id: number, version: number): Promise<WorkflowVersion> {
    return this.request<WorkflowVersion>(
      "GET",
      `/api/v1/workflows/${id}/versions/${version}`
    );
  }

  lock(id: number, sessionId: string): Promise<{ ok?: boolean }> {
    return this.request("POST", `/api/v1/workflows/${id}/lock`, {
      session_id: sessionId,
    });
  }

  unlock(id: number, sessionId: string): Promise<unknown> {
    return this.request("DELETE", `/api/v1/workflows/${id}/lock`, {
      session_id: sessionId,
    });
  }

  listJsonSchemas(): Promise<JsonSchema[]> {
    return this.request<JsonSchema[]>("GET", "/api/v1/json-schemas");
  }

  listStoredProcedures(workflowId?: number): Promise<StoredProcedure[]> {
    const qs = workflowId !== undefined ? `?workflow_id=${workflowId}` : "";
    return this.request<StoredProcedure[]>(
      "GET",
      `/api/v1/stored-procedures${qs}`
    );
  }

  listCannedScripts(): Promise<CannedScript[]> {
    return this.request<CannedScript[]>("GET", "/api/v1/canned-scripts");
  }

  listCannedQueries(): Promise<CannedQuery[]> {
    return this.request<CannedQuery[]>("GET", "/api/v1/canned-queries");
  }

  // The devlake-db endpoints report SQL errors as HTTP 200 with an `error` field.
  async devlakeQuery(sql: string): Promise<DevLakeQueryResult> {
    const resp = await this.request<Partial<DevLakeQueryResult> & { error?: string }>(
      "POST",
      "/api/v1/devlake-db/query",
      { sql }
    );
    if (resp.error) {
      throw new Error(resp.error);
    }
    return { columns: resp.columns || [], rows: resp.rows || [] };
  }

  async devlakeTables(): Promise<string[]> {
    const resp = await this.request<{ tables?: string[]; error?: string }>(
      "GET",
      "/api/v1/devlake-db/tables"
    );
    if (resp.error) {
      throw new Error(resp.error);
    }
    return resp.tables || [];
  }

  runTests(id: number): Promise<{ results: TestCaseResult[] }> {
    return this.request("POST", `/api/v1/workflows/${id}/run-tests`);
  }

  runWorkflow(id: number, input: unknown): Promise<WorkflowResult> {
    return this.request<WorkflowResult>(
      "POST",
      `/api/v1/workflows/${id}/test`,
      input
    );
  }

  runStage(
    req: RunStageRequest
  ): Promise<{ output?: unknown; error?: string; logs?: LogEntry[] }> {
    return this.request("POST", "/api/v1/workflows/run-stage", req);
  }

  publishWorkflow(
    id: number,
    publishNote?: string
  ): Promise<Workflow> {
    return this.request<Workflow>(
      "POST",
      `/api/v1/workflows/${id}/publish`,
      publishNote ? { publish_note: publishNote } : {}
    );
  }

  setProductionVersion(
    id: number,
    version: number
  ): Promise<{ status: string; version: number }> {
    return this.request(
      "POST",
      `/api/v1/workflows/${id}/versions/${version}/production`
    );
  }
}
