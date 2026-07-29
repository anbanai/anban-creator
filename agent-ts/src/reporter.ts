import { createReadStream } from "node:fs";

import type { JobConfig } from "./config.js";

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
  [key: string]: unknown;
}

export interface ArtifactPrepareRequest {
  relative_path: string;
  filename: string;
  content_type: string;
  size: number;
  sha256: string;
}

export interface ArtifactPrepareResponse {
  upload_required: boolean;
  key: string;
  upload_url?: string;
  method?: string;
  headers?: Record<string, string>;
  max_size?: number;
  endpoint?: string;
  bucket?: string;
  sts_access_key_id?: string;
  sts_access_key_secret?: string;
  sts_security_token?: string;
}

export interface ArtifactManifestFile {
  relative_path: string;
  object_key: string;
  content_type: string;
  size: number;
  sha256: string;
  role?: string;
}

type Sleep = (milliseconds: number) => Promise<void>;
const sleep: Sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

export async function postJSONWithRetry(operation: () => Promise<void>, wait: Sleep = sleep): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await operation();
      return;
    } catch (error) {
      lastError = error;
      if (attempt === 2) break;
      await wait(250 * 2 ** attempt);
    }
  }
  throw lastError;
}

export class Reporter {
  constructor(private readonly config: JobConfig, private readonly token: string, private readonly taskID: string) {}

  async progress(message: string, signal?: AbortSignal): Promise<void> {
    if (!message.trim()) return;
    await this.post("/api/v1/agent/progress", this.identity({ message: message.trim() }), signal);
  }

  async heartbeat(signal?: AbortSignal): Promise<void> {
    await this.post("/api/v1/agent/progress", this.identity({}), signal);
  }

  async complete(result: ExecutionResult, signal?: AbortSignal): Promise<void> {
    await postJSONWithRetry(() => this.post("/api/v1/agent/complete", this.identity({ result }), signal));
  }

  async prepareArtifactUpload(request: ArtifactPrepareRequest, signal?: AbortSignal): Promise<ArtifactPrepareResponse> {
    return this.postJSON("/api/v1/agent/artifacts/prepare", this.identity(request), signal);
  }

  async streamArtifactContent(metadata: Omit<ArtifactPrepareRequest, "filename">, body: ReadableStream<Uint8Array>, signal?: AbortSignal): Promise<{ object_key: string; content_type: string; size: number; sha256: string }> {
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
      body,
      signal,
      duplex: "half",
    } as RequestInit);
    if (!response.ok) throw new Error(`/api/v1/agent/artifacts/content returned HTTP ${response.status}`);
    return this.decodeEnvelope(await response.text(), "/api/v1/agent/artifacts/content");
  }

  async reportArtifactManifest(files: ArtifactManifestFile[], signal?: AbortSignal): Promise<void> {
    await this.post("/api/v1/agent/artifacts/manifest", this.identity({ files }), signal);
  }

  async putPreparedArtifact(prepared: ArtifactPrepareResponse, path: string, contentType: string, sha256: string, signal?: AbortSignal): Promise<void> {
    if (!prepared.upload_required) return;
    if (!prepared.upload_url) throw new Error("direct artifact upload requires a signed upload URL");
    const headers = { ...(prepared.headers ?? {}) };
    if (headers["X-Oss-Meta-Sha256"]?.toLowerCase() !== sha256) throw new Error("direct artifact upload is missing SHA-256 metadata");
    if (!headers["Content-Type"]) headers["Content-Type"] = contentType;
    const response = await fetch(prepared.upload_url, { method: prepared.method || "PUT", headers, body: createReadStream(path) as unknown as BodyInit, signal, duplex: "half" } as RequestInit);
    if (!response.ok) throw new Error(`direct artifact upload returned HTTP ${response.status}`);
  }

  private identity<T extends object>(body: T): T & { task_id: string; execution_id: string } {
    return { ...body, task_id: this.taskID, execution_id: this.config.executionID };
  }

  private async post(path: string, body: unknown, signal?: AbortSignal): Promise<void> { await this.postJSON(path, body, signal); }

  private async postJSON<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
    const response = await fetch(`${this.config.serverURL}${path}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.token}`, "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal,
      redirect: "error",
    });
    if (!response.ok) throw new Error(`${path} returned HTTP ${response.status}`);
    return this.decodeEnvelope(await response.text(), path);
  }

  private decodeEnvelope<T>(raw: string, path: string): T {
    if (!raw.trim()) return undefined as T;
    let envelope: { code?: number; msg?: string; data?: T };
    try { envelope = JSON.parse(raw) as typeof envelope; } catch { throw new Error(`${path} returned invalid JSON`); }
    if (envelope.code !== undefined && envelope.code !== 0) throw new Error(`${path} failed: ${envelope.msg ?? "unknown error"}`);
    return envelope.data as T;
  }
}
