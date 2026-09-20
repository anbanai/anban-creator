import { retryRequest, transportErrorFromResponse, TypedTransportError, type TransferOperation } from "./transport.js";

export interface ReporterConfig {
  serverURL: string;
  executionID: string;
}

export interface ExecutionResult {
  success: boolean;
  error?: string;
  work_dir?: string;
  session_id?: string;
  result_subtype?: string;
  num_turns?: number;
  duration_ms?: number;
  log_text?: string;
  model_usage?: unknown[];
  tool_use_count?: number;
  tool_use_summary?: Record<string, number>;
  [key: string]: unknown;
}

export interface ArtifactPrepareRequest {
  relative_path: string;
  filename: string;
  content_type: string;
  size: number;
  sha256: string;
}

export type ArtifactPrepareResponse =
  | { upload_required: false; key: string }
  | {
      upload_required: true;
      key: string;
      upload_url: string;
      method: "PUT";
      headers: Record<string, string>;
      expires_at: string;
      max_size: number;
    };

export interface ArtifactManifestFile {
  relative_path: string;
  object_key: string;
  content_type: string;
  size: number;
  sha256: string;
  role?: string;
}

export interface StageProgressEvent {
  stage: string;
  state: "active" | "complete";
  description?: string;
}

export type StreamBodyFactory = (signal: AbortSignal) => Promise<ReadableStream<Uint8Array>>;

const DEFAULT_SIGNAL = new AbortController().signal;
const PROGRESS_ERROR_CODES = new Set(["progress_unknown_stage", "progress_out_of_order", "progress_invalid_state"]);
const MAX_ERROR_RESPONSE_BYTES = 8 * 1024;

export class Reporter {
  constructor(private readonly config: ReporterConfig, private readonly token: string, private readonly taskID: string) {}

  async progress(message: string, signal: AbortSignal = DEFAULT_SIGNAL): Promise<void> {
    if (!message.trim()) return;
    await this.postJSON("progress", "/api/v1/agent/progress", this.identity({ message: message.trim() }), signal);
  }

  async stageProgress(event: StageProgressEvent, signal: AbortSignal = DEFAULT_SIGNAL): Promise<void> {
    await this.postJSON("progress", "/api/v1/agent/progress", this.identity({
      stage: event.stage,
      state: event.state,
      description: event.description ?? "",
    }), signal);
  }

  async heartbeat(signal: AbortSignal = DEFAULT_SIGNAL): Promise<void> {
    await this.postJSON("progress", "/api/v1/agent/progress", this.identity({}), signal);
  }

  async complete(result: ExecutionResult, signal: AbortSignal = DEFAULT_SIGNAL): Promise<void> {
    await this.postJSON("complete", "/api/v1/agent/complete", this.identity({ result }), signal);
  }

  async submitCompletionMetadata(metadata: unknown, signal: AbortSignal = DEFAULT_SIGNAL): Promise<void> {
    await retryRequest("complete", (requestSignal) => this.callMCP("submit_completion_metadata", {
      task_id: this.taskID,
      execution_id: this.config.executionID,
      metadata: JSON.stringify(metadata),
    }, requestSignal), { signal, timeoutMs: 30_000, maxAttempts: 4 });
  }

  async prepareArtifactUpload(request: ArtifactPrepareRequest, signal: AbortSignal = DEFAULT_SIGNAL, deadlineAt?: number): Promise<ArtifactPrepareResponse> {
    const prepared = await this.postJSON<unknown>("prepare", "/api/v1/agent/artifacts/prepare", this.identity(request), signal, 15_000, deadlineAt);
    return validateArtifactPrepareResponse(prepared);
  }

  async streamArtifactContent(
    metadata: Omit<ArtifactPrepareRequest, "filename">,
    body: StreamBodyFactory,
    signal: AbortSignal = DEFAULT_SIGNAL,
    deadlineAt?: number,
  ): Promise<{ object_key: string; content_type: string; size: number; sha256: string }> {
    return retryRequest("stream", async (requestSignal) => {
      const requestBody = await body(requestSignal);
      try {
        const response = await fetch(`${this.config.serverURL}/api/v1/agent/artifacts/content`, {
          method: "POST",
          headers: {
            Authorization: `Bearer ${this.token}`,
            "Content-Type": metadata.content_type,
            "X-Anban-Artifact-Path": metadata.relative_path,
            "X-Anban-Artifact-Size": String(metadata.size),
            "X-Anban-Artifact-SHA256": metadata.sha256,
            "X-Anban-Task-ID": this.taskID,
            "X-Anban-Execution-ID": this.config.executionID,
          },
          body: requestBody,
          signal: requestSignal,
          duplex: "half",
          redirect: "error",
        } as RequestInit);
        if (!response.ok) throw await responseError("stream", response);
        return this.decodeEnvelope(await response.text(), "stream");
      } finally {
        await requestBody.cancel().catch(() => {});
      }
    }, { signal, deadlineAt, maxAttempts: 4 });
  }

  async reportArtifactManifest(files: ArtifactManifestFile[], signal: AbortSignal = DEFAULT_SIGNAL, deadlineAt?: number): Promise<void> {
    await this.postJSON("manifest", "/api/v1/agent/artifacts/manifest", this.identity({ files }), signal, 30_000, deadlineAt);
  }

  private identity<T extends object>(body: T): T & { task_id: string; execution_id?: string } {
    return { ...body, task_id: this.taskID, ...(this.config.executionID ? { execution_id: this.config.executionID } : {}) };
  }

  private async postJSON<T>(operation: TransferOperation, path: string, body: unknown, signal: AbortSignal, timeoutMs?: number, deadlineAt?: number): Promise<T> {
    const encoded = JSON.stringify(body);
    return retryRequest(operation, async (requestSignal) => {
      const response = await fetch(`${this.config.serverURL}${path}`, {
        method: "POST",
        headers: { Authorization: `Bearer ${this.token}`, "Content-Type": "application/json" },
        body: encoded,
        signal: requestSignal,
        redirect: "error",
      });
      if (!response.ok) throw await responseError(operation, response);
      return this.decodeEnvelope(await response.text(), operation);
    }, { signal, timeoutMs, deadlineAt, maxAttempts: 4 });
  }

  private async callMCP(tool: string, arguments_: Record<string, unknown>, signal: AbortSignal): Promise<void> {
    const endpoint = `${this.config.serverURL}/mcp`;
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.token}`,
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
    };
    const initialize = await fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "anban-runtime", version: "1.0" } } }),
      signal,
    });
    if (!initialize.ok) throw await responseError("complete", initialize);
    await initialize.arrayBuffer();
    const session = initialize.headers.get("mcp-session-id");
    if (session) headers["Mcp-Session-Id"] = session;
    const initialized = await fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }),
      signal,
    });
    if (!initialized.ok) throw await responseError("complete", initialized);
    await initialized.arrayBuffer();
    const response = await fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: tool, arguments: arguments_ } }),
      signal,
    });
    if (!response.ok) throw await responseError("complete", response);
    const raw = await response.text();
    if (!raw.trim()) return;
    let envelope: { error?: unknown; result?: { isError?: boolean } };
    try {
      envelope = JSON.parse(raw) as typeof envelope;
    } catch {
      throw protocolFailure("complete");
    }
    if (envelope.error || envelope.result?.isError) throw protocolFailure("complete");
  }

  private decodeEnvelope<T>(raw: string, operation: TransferOperation): T {
    if (!raw.trim()) return undefined as T;
    let envelope: { code?: number; data?: T };
    try {
      envelope = JSON.parse(raw) as typeof envelope;
    } catch {
      throw protocolFailure(operation);
    }
    if (envelope.code !== undefined && envelope.code !== 0) throw protocolFailure(operation);
    return envelope.data as T;
  }
}

async function responseError(operation: TransferOperation, response: Response): Promise<TypedTransportError> {
  const serverCode = operation === "progress" ? await safeProgressErrorCode(response) : undefined;
  if (operation !== "progress") await response.body?.cancel().catch(() => {});
  return transportErrorFromResponse(operation, response, undefined, serverCode);
}

async function safeProgressErrorCode(response: Response): Promise<string | undefined> {
  if (!response.body) return undefined;
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (size <= MAX_ERROR_RESPONSE_BYTES) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_ERROR_RESPONSE_BYTES) return undefined;
      chunks.push(value);
    }
  } finally {
    await reader.cancel().catch(() => {});
  }
  try {
    const raw = Buffer.concat(chunks).toString("utf8");
    const code = (JSON.parse(raw) as { error_code?: unknown }).error_code;
    return typeof code === "string" && PROGRESS_ERROR_CODES.has(code) ? code : undefined;
  } catch {
    return undefined;
  }
}

function protocolFailure(operation: TransferOperation): TypedTransportError {
  return new TypedTransportError({ operation, code: "protocol_error", attempts: 1, retryable: false });
}

function validateArtifactPrepareResponse(value: unknown): ArtifactPrepareResponse {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw protocolFailure("prepare");
  const response = value as Record<string, unknown>;
  if (typeof response.key !== "string" || !response.key || typeof response.upload_required !== "boolean") throw protocolFailure("prepare");
  const forbidden = ["endpoint", "bucket", "sts_access_key_id", "sts_access_key_secret", "sts_security_token"];
  if (forbidden.some((field) => field in response)) throw protocolFailure("prepare");
  if (!response.upload_required) return { upload_required: false, key: response.key };
  if (response.method !== "PUT" || typeof response.upload_url !== "string" || typeof response.expires_at !== "string"
    || typeof response.max_size !== "number" || !Number.isSafeInteger(response.max_size) || response.max_size < 0
    || !response.headers || typeof response.headers !== "object" || Array.isArray(response.headers)
    || !Object.values(response.headers).every((header) => typeof header === "string")) throw protocolFailure("prepare");
  let url: URL;
  try { url = new URL(response.upload_url); } catch { throw protocolFailure("prepare"); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || !Number.isFinite(Date.parse(response.expires_at))) throw protocolFailure("prepare");
  return {
    upload_required: true,
    key: response.key,
    upload_url: response.upload_url,
    method: "PUT",
    headers: response.headers as Record<string, string>,
    expires_at: response.expires_at,
    max_size: response.max_size,
  };
}
