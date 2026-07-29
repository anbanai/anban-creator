import { lstat, readFile, realpath } from "node:fs/promises";
import { dirname } from "node:path";
import { isAbsolute, resolve } from "node:path";

import type { JobConfig } from "./config.js";

const MAX_WORKLOAD_TOKEN_BYTES = 16 << 10;
const MAX_BOOTSTRAP_RESPONSE_BYTES = 2 << 20;
const MAX_BOOTSTRAP_FILE_BYTES = 64 << 20;
const MAX_BOOTSTRAP_FILES = 256;
const MAX_BOOTSTRAP_TURNS = 1000;
const MAX_BOOTSTRAP_PROMPT_BYTES = 1 << 20;
const MAX_EXECUTION_TOKEN_BYTES = 16 << 10;

export interface BootstrapFile {
  path: string;
  text?: string;
  download_url?: string;
  mode: number;
  expected_size?: number;
  max_bytes?: number;
}

export interface BootstrapResponse {
  execution_token: string;
  task_id: string;
  task_type: string;
  project_id: string;
  prompt: string;
  model?: string;
  max_turns: number;
  agent_flag: string;
  auto_memory_directory?: string;
  resume_session_id?: string;
  resume_context_path?: string;
  runtime_env?: Record<string, string>;
  model_usage_aliases?: Record<string, { provider: string; model: string }>;
  env?: Record<string, string>;
  files?: BootstrapFile[];
  artifact_transport: { mode: "direct" | "stream" };
}

export async function readWorkloadToken(path: string): Promise<string> {
  const visible = resolve(path);
  const root = await realpath(dirname(visible));
  const resolved = await realpath(visible);
  if (!resolved.startsWith(`${root}/`)) throw new Error("workload token escapes projected volume");
  const info = await lstat(resolved);
  if (!info.isFile() || info.isSymbolicLink() || info.size > MAX_WORKLOAD_TOKEN_BYTES || (info.mode & 0o022) !== 0) throw new Error("workload token is invalid");
  const token = (await readFile(resolved, "utf8")).trim();
  if (!token || Buffer.byteLength(token) > MAX_WORKLOAD_TOKEN_BYTES || /\s/.test(token)) throw new Error("workload token is empty or malformed");
  return token;
}

export async function bootstrap(config: JobConfig, token: string, signal?: AbortSignal): Promise<BootstrapResponse> {
  const response = await fetch(`${config.serverURL}/api/v1/agent/bootstrap`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ execution_id: config.executionID }),
    signal,
    redirect: "error",
  });
  if (!response.ok) throw new Error(`bootstrap returned HTTP ${response.status}`);
  const body = await readBoundedText(response, MAX_BOOTSTRAP_RESPONSE_BYTES, "bootstrap response");
  let envelope: { code: number; msg?: string; data?: BootstrapResponse };
  try {
    envelope = JSON.parse(body) as typeof envelope;
  } catch {
    throw new Error("bootstrap response is not valid JSON");
  }
  if (envelope.code !== 0 || !envelope.data) throw new Error("bootstrap request rejected");
  return validateBootstrapResponse(config.executionID, envelope.data);
}

export function validateBootstrapResponse(executionID: string, data: BootstrapResponse): BootstrapResponse {
  if (!data || !cleanString(data.execution_token) || !cleanString(data.task_id) || !cleanString(data.project_id)) {
    throw new Error("bootstrap response identity is incomplete");
  }
  validateExecutionToken(executionID, data);
  if (!new Set(["article", "seednote", "moments", "ecommerce", "montage", "live-slicer"]).has(data.task_type)) throw new Error("bootstrap task type is invalid");
  if (data.artifact_transport?.mode !== "direct" && data.artifact_transport?.mode !== "stream") throw new Error("bootstrap artifact transport is invalid");
  if (!cleanString(data.prompt) || Buffer.byteLength(data.prompt) > MAX_BOOTSTRAP_PROMPT_BYTES) throw new Error("bootstrap prompt is invalid");
  if (!Number.isInteger(data.max_turns) || data.max_turns < 1 || data.max_turns > MAX_BOOTSTRAP_TURNS) throw new Error("bootstrap max turns is invalid");
  if (data.agent_flag !== `anban:${data.task_type}`) throw new Error("bootstrap agent flag is invalid");
  if (data.auto_memory_directory !== ".claude/memory") throw new Error("bootstrap auto memory directory is invalid");
  if (data.model !== undefined && (!cleanString(data.model) || Buffer.byteLength(data.model) > 256)) throw new Error("bootstrap model is invalid");
  if (data.resume_session_id && (!cleanString(data.resume_session_id) || data.resume_session_id.length > 128 || /[\s\x00-\x1f]/.test(data.resume_session_id))) throw new Error("bootstrap resume session ID is invalid");
  if (data.resume_session_id && !data.resume_context_path) throw new Error("bootstrap resume session requires resume context");
  if (!data.model_usage_aliases || Object.keys(data.model_usage_aliases).length === 0 || !Object.values(data.model_usage_aliases).every((identity) => cleanString(identity.provider) && cleanString(identity.model))) throw new Error("bootstrap model usage aliases are invalid");
  for (const [key, value] of Object.entries(data.runtime_env ?? {})) validateEnvironmentEntry(key, value, "runtime");
  if (data.task_type !== "montage" && Object.keys(data.env ?? {}).length > 0) throw new Error("bootstrap environment is only valid for Montage tasks");
  for (const [key, value] of Object.entries(data.env ?? {})) validateEnvironmentEntry(key, value, "Montage");
  preflightBootstrapFiles(data.files ?? []);
  return data;
}

export function preflightBootstrapFiles(files: BootstrapFile[]): BootstrapFile[] {
  if (files.length > MAX_BOOTSTRAP_FILES) throw new Error("bootstrap file count exceeds limit");
  const seen = new Map<string, string>();
  for (const file of files) {
    const relative = cleanBootstrapPath(file.path);
    const key = relative.toLowerCase();
    if (seen.has(key)) throw new Error(`duplicate bootstrap path ${relative} conflicts with ${seen.get(key)}`);
    if (key === ".claude/memory" || key.startsWith(".claude/memory/")) throw new Error(`bootstrap path ${relative} targets protected auto memory`);
    seen.set(key, relative);
    const inline = file.text !== undefined;
    const remote = cleanString(file.download_url ?? "");
    if (inline === remote) throw new Error(`bootstrap file ${relative} must have exactly one content source`);
    const maxBytes = file.max_bytes ?? MAX_BOOTSTRAP_FILE_BYTES;
    if (!Number.isInteger(maxBytes) || maxBytes < 1 || maxBytes > MAX_BOOTSTRAP_FILE_BYTES || (file.expected_size !== undefined && (!Number.isInteger(file.expected_size) || file.expected_size < 0 || file.expected_size > maxBytes))) throw new Error(`bootstrap file ${relative} has invalid size limits`);
    if (!Number.isInteger(file.mode) || ![0o600, 0o644].includes(file.mode)) throw new Error(`unsafe bootstrap file mode ${file.mode}`);
    if (inline && Buffer.byteLength(file.text ?? "") > maxBytes) throw new Error(`bootstrap file ${relative} exceeds size limit`);
    if (remote) validateBootstrapDownloadURL(file.download_url!);
  }
  for (const [key, relative] of seen) {
    for (let offset = key.lastIndexOf("/"); offset >= 0; offset = key.lastIndexOf("/", offset - 1)) {
      const ancestor = key.slice(0, offset);
      if (seen.has(ancestor)) throw new Error(`bootstrap path ${relative} conflicts with file path ${seen.get(ancestor)}`);
    }
  }
  return files;
}

export function cleanBootstrapPath(raw: string): string {
  if (!cleanString(raw) || isAbsolute(raw) || raw.includes("\\") || /^[a-zA-Z]:/.test(raw)) throw new Error(`bootstrap path ${raw} must be clean and relative`);
  const components = raw.split("/");
  if (components.some((part) => !part || part === "." || part === ".." || Buffer.byteLength(part) > 255 || /[\x00-\x1f<>:"\\|?*]/.test(part) || /[. ]$/.test(part))) throw new Error(`bootstrap path ${raw} escapes workspace or is not clean`);
  return raw;
}

export function workspacePath(workspace: string, relativePath: string): string {
  const relative = cleanBootstrapPath(relativePath);
  const root = resolve(workspace);
  const target = resolve(root, relative);
  if (!target.startsWith(`${root}/`)) throw new Error(`unsafe workspace path ${relativePath}`);
  return target;
}

export async function readBoundedText(response: Response, limit: number, label: string): Promise<string> {
  const reader = response.body?.getReader();
  if (!reader) return "";
  const chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > limit) throw new Error(`${label} exceeds size limit`);
    chunks.push(value);
  }
  return new TextDecoder().decode(Buffer.concat(chunks));
}

function validateExecutionToken(executionID: string, data: BootstrapResponse): void {
  const token = data.execution_token;
  if (Buffer.byteLength(token) > MAX_EXECUTION_TOKEN_BYTES || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) throw new Error("bootstrap execution token is malformed");
  try {
    const claims = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8")) as Record<string, unknown>;
    if (claims.execution_id !== executionID || claims.task_id !== data.task_id || claims.project_id !== data.project_id) throw new Error("bootstrap response identity mismatch");
  } catch (error) {
    if (error instanceof Error && error.message === "bootstrap response identity mismatch") throw error;
    throw new Error("bootstrap execution token is malformed");
  }
}

function validateBootstrapDownloadURL(raw: string): void {
  let url: URL;
  try { url = new URL(raw); } catch { throw new Error("unsafe bootstrap download URL"); }
  if (url.protocol !== "https:" || url.username || url.password || url.hash || !url.hostname) throw new Error("unsafe bootstrap download URL");
}

function validateEnvironmentEntry(key: string, value: string, label: string): void {
  if (!/^[A-Z_][A-Z0-9_]*$/.test(key) || !cleanString(value) || Buffer.byteLength(value) > 16 << 10) throw new Error(`bootstrap ${label} environment is invalid`);
}

function cleanString(value: string): boolean { return value.length > 0 && value.trim() === value; }
