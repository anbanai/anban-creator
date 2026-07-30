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
const MAX_MODEL_USAGE_ALIASES = 128;

const BOOTSTRAP_RESPONSE_KEYS = [
  "execution_token", "task_id", "task_type", "project_id", "prompt",
  "execution_profile", "max_turns", "agent_flag", "auto_memory_directory",
  "resume_session_id", "resume_context_path", "env", "files", "artifact_transport",
];

const EXECUTION_PROFILE_KEYS = [
  "profile_id", "provider", "protocol", "models", "claude", "display_name",
  "profile_fingerprint", "runtime_env", "model_usage_aliases",
];

const EXECUTION_PROFILE_IDS = new Set(["cost_effective", "balanced", "maximum_quality"]);

const MODEL_ENV_KEYS: Record<keyof AgentModelMatrix, string> = {
  default: "ANTHROPIC_MODEL",
  opus: "ANTHROPIC_DEFAULT_OPUS_MODEL",
  fable: "ANTHROPIC_DEFAULT_FABLE_MODEL",
  sonnet: "ANTHROPIC_DEFAULT_SONNET_MODEL",
  haiku: "ANTHROPIC_DEFAULT_HAIKU_MODEL",
};

const CLAUDE_CONTROL_ENV_KEYS: Record<keyof AgentClaudeControls, string> = {
  effort_level: "CLAUDE_CODE_EFFORT_LEVEL",
  always_enable_effort: "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
  max_context_tokens: "CLAUDE_CODE_MAX_CONTEXT_TOKENS",
  max_output_tokens: "CLAUDE_CODE_MAX_OUTPUT_TOKENS",
  max_thinking_tokens: "MAX_THINKING_TOKENS",
  disable_adaptive_thinking: "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
  disable_thinking: "CLAUDE_CODE_DISABLE_THINKING",
  auto_compact_window: "CLAUDE_CODE_AUTO_COMPACT_WINDOW",
  autocompact_pct_override: "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE",
  disable_1m_context: "CLAUDE_CODE_DISABLE_1M_CONTEXT",
  subagent_model: "CLAUDE_CODE_SUBAGENT_MODEL",
  enable_tool_search: "ENABLE_TOOL_SEARCH",
};

const CLAUDE_RUNTIME_ENV_KEYS = new Set([
  "ANTHROPIC_AUTH_TOKEN",
  "ANTHROPIC_BASE_URL",
  "ANTHROPIC_MODEL",
  "ANTHROPIC_DEFAULT_OPUS_MODEL",
  "ANTHROPIC_DEFAULT_FABLE_MODEL",
  "ANTHROPIC_DEFAULT_SONNET_MODEL",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL",
  ...Object.values(CLAUDE_CONTROL_ENV_KEYS),
]);

export interface BootstrapFile {
  path: string;
  text?: string;
  download_url?: string;
  mode: number;
  expected_size?: number;
  max_bytes?: number;
}

export interface AgentModelMatrix {
  default: string;
  opus: string;
  fable: string;
  sonnet: string;
  haiku: string;
}

export interface AgentClaudeControls {
  effort_level?: "low" | "medium" | "high" | "max";
  always_enable_effort?: boolean;
  max_context_tokens?: number;
  max_output_tokens?: number;
  max_thinking_tokens?: number;
  disable_adaptive_thinking?: boolean;
  disable_thinking?: boolean;
  auto_compact_window?: number;
  autocompact_pct_override?: number;
  disable_1m_context?: boolean;
  subagent_model?: string;
  enable_tool_search?: boolean;
}

export interface ExecutionProfile {
  profile_id: string;
  provider: string;
  protocol: string;
  models: AgentModelMatrix;
  claude: AgentClaudeControls;
  display_name: string;
  profile_fingerprint: string;
  runtime_env: Record<string, string>;
  model_usage_aliases: Record<string, { provider: string; model: string }>;
}

export interface BootstrapResponse {
  execution_token: string;
  task_id: string;
  task_type: string;
  project_id: string;
  prompt: string;
  execution_profile: ExecutionProfile;
  max_turns: number;
  agent_flag: string;
  auto_memory_directory?: string;
  resume_session_id?: string;
  resume_context_path?: string;
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
  let envelope: { code: number; msg?: string; data?: unknown };
  try {
    envelope = JSON.parse(body) as typeof envelope;
  } catch {
    throw new Error("bootstrap response is not valid JSON");
  }
  if (envelope.code !== 0 || !envelope.data) throw new Error("bootstrap request rejected");
  return validateBootstrapResponse(config.executionID, envelope.data);
}

export function validateBootstrapResponse(executionID: string, input: unknown): BootstrapResponse {
  if (isRecord(input) && ["model", "runtime_env", "model_usage_aliases"].some((field) => field in input)) {
    throw new Error("bootstrap response contains legacy top-level model fields");
  }
  if (!isRecord(input) || !hasOnlyKeys(input, BOOTSTRAP_RESPONSE_KEYS)) {
    throw new Error("bootstrap response contains unknown fields");
  }
  const data = input as unknown as BootstrapResponse;
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
  validateExecutionProfile(data.execution_profile);
  if (data.resume_session_id && (!cleanString(data.resume_session_id) || data.resume_session_id.length > 128 || /[\s\x00-\x1f]/.test(data.resume_session_id))) throw new Error("bootstrap resume session ID is invalid");
  if (data.resume_session_id && !data.resume_context_path) throw new Error("bootstrap resume session requires resume context");
  if (data.task_type !== "montage" && Object.keys(data.env ?? {}).length > 0) throw new Error("bootstrap environment is only valid for Montage tasks");
  for (const [key, value] of Object.entries(data.env ?? {})) validateEnvironmentEntry(key, value, "Montage");
  preflightBootstrapFiles(data.files ?? []);
  return data;
}

function validateExecutionProfile(input: unknown): asserts input is ExecutionProfile {
  if (!isRecord(input)) throw new Error("bootstrap execution profile is invalid");
  const profile = input as Record<string, unknown>;
  if ("model_id" in profile || "context_window" in profile || "reasoning_effort" in profile || "thinking_required" in profile) {
    throw new Error("bootstrap execution profile contains legacy single-model fields");
  }
  if (!hasOnlyKeys(profile, EXECUTION_PROFILE_KEYS)) {
    throw new Error("bootstrap execution profile contains unknown fields");
  }
  if (!cleanString(profile.profile_id) || !EXECUTION_PROFILE_IDS.has(profile.profile_id) || !cleanString(profile.provider) || /[\x00\r\n/]/.test(profile.provider) || !cleanString(profile.display_name)) {
    throw new Error("bootstrap execution profile identity is invalid");
  }
  if (profile.protocol !== "anthropic") throw new Error("bootstrap execution profile protocol is invalid");
  if (!/^[0-9a-f]{64}$/.test(String(profile.profile_fingerprint ?? ""))) throw new Error("bootstrap execution profile fingerprint is invalid");
  const models = validateModelMatrix(profile.models);
  const controls = validateClaudeControls(profile.claude);
  if (!isRecord(profile.runtime_env)) throw new Error("bootstrap execution profile runtime environment is invalid");
  for (const [key, value] of Object.entries(profile.runtime_env)) {
    if (!CLAUDE_RUNTIME_ENV_KEYS.has(key)) throw new Error("bootstrap execution profile runtime environment is invalid");
    validateEnvironmentEntry(key, value, "runtime");
  }
  if (!validProviderBaseURL(profile.runtime_env.ANTHROPIC_BASE_URL) || !cleanString(profile.runtime_env.ANTHROPIC_AUTH_TOKEN)) throw new Error("bootstrap execution profile runtime environment is invalid");
  for (const [role, envKey] of Object.entries(MODEL_ENV_KEYS) as Array<[keyof AgentModelMatrix, string]>) {
    if (profile.runtime_env[envKey] !== models[role]) throw new Error("bootstrap execution profile runtime model is invalid");
  }
  for (const [control, envKey] of Object.entries(CLAUDE_CONTROL_ENV_KEYS) as Array<[keyof AgentClaudeControls, string]>) {
    const configured = controls[control];
    const emitted = profile.runtime_env[envKey];
    if (configured === undefined ? emitted !== undefined : emitted !== String(configured)) {
      throw new Error("bootstrap execution profile runtime environment is invalid");
    }
  }
  const aliases = profile.model_usage_aliases;
  const requiredAliases = new Set(Object.values(models));
  if (controls.subagent_model) requiredAliases.add(controls.subagent_model);
  if (!isRecord(aliases) || Object.keys(aliases).length > MAX_MODEL_USAGE_ALIASES || ![...requiredAliases].every((raw) => isRecord(aliases[raw]))) throw new Error("bootstrap execution profile model usage aliases are invalid");
  if (!Object.entries(aliases).every(([raw, identity]) => isRecord(identity) && hasOnlyKeys(identity, ["provider", "model"]) && validModelUsageAlias(raw, identity.provider, identity.model) && identity.provider === profile.provider)) {
    throw new Error("bootstrap execution profile model usage aliases are invalid");
  }
}

function validateModelMatrix(input: unknown): AgentModelMatrix {
  if (!isRecord(input) || !hasOnlyKeys(input, Object.keys(MODEL_ENV_KEYS))) throw new Error("bootstrap execution profile model matrix is invalid");
  const models = input as Record<keyof AgentModelMatrix, unknown>;
  for (const role of Object.keys(MODEL_ENV_KEYS) as Array<keyof AgentModelMatrix>) {
    if (!cleanString(models[role]) || /[\x00\r\n]/.test(models[role])) throw new Error("bootstrap execution profile model matrix is invalid");
  }
  return models as AgentModelMatrix;
}

function validateClaudeControls(input: unknown): AgentClaudeControls {
  if (!isRecord(input) || !hasOnlyKeys(input, Object.keys(CLAUDE_CONTROL_ENV_KEYS))) throw new Error("bootstrap execution profile Claude controls are invalid");
  const controls = input as Record<string, unknown>;
  if (controls.effort_level !== undefined && !new Set(["low", "medium", "high", "max"]).has(String(controls.effort_level))) throw new Error("bootstrap execution profile Claude controls are invalid");
  for (const field of ["always_enable_effort", "disable_adaptive_thinking", "disable_thinking", "disable_1m_context", "enable_tool_search"]) {
    if (controls[field] !== undefined && typeof controls[field] !== "boolean") throw new Error("bootstrap execution profile Claude controls are invalid");
  }
  for (const field of ["max_context_tokens", "max_output_tokens", "auto_compact_window"]) {
    if (controls[field] !== undefined && (!Number.isInteger(controls[field]) || (controls[field] as number) <= 0)) throw new Error("bootstrap execution profile Claude controls are invalid");
  }
  if (controls.max_thinking_tokens !== undefined && (!Number.isInteger(controls.max_thinking_tokens) || (controls.max_thinking_tokens as number) < 0)) throw new Error("bootstrap execution profile Claude controls are invalid");
  if (controls.autocompact_pct_override !== undefined && (!Number.isInteger(controls.autocompact_pct_override) || (controls.autocompact_pct_override as number) < 1 || (controls.autocompact_pct_override as number) > 100)) throw new Error("bootstrap execution profile Claude controls are invalid");
  if (controls.subagent_model !== undefined && (!cleanString(controls.subagent_model) || /[\x00\r\n]/.test(controls.subagent_model))) throw new Error("bootstrap execution profile Claude controls are invalid");
  return controls as AgentClaudeControls;
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: string[]): boolean {
  const keys = Object.keys(value);
  return keys.length <= allowed.length && keys.every((key) => allowed.includes(key));
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

function validateEnvironmentEntry(key: string, value: unknown, label: string): void {
  if (!/^[A-Z_][A-Z0-9_]*$/.test(key) || !cleanString(value) || Buffer.byteLength(value) > 16 << 10) throw new Error(`bootstrap ${label} environment is invalid`);
}

function validProviderBaseURL(raw: unknown): boolean {
  if (!cleanString(raw)) return false;
  try {
    const url = new URL(raw);
    return url.protocol === "https:" && Boolean(url.hostname) && !url.username && !url.password && !url.search && !url.hash;
  } catch {
    return false;
  }
}

function validModelUsageAlias(raw: string, provider: unknown, model: unknown): provider is string {
  return cleanString(raw) && !/[\x00\r\n=]/.test(raw) && cleanString(provider) && !/[\x00\r\n/]/.test(provider) && cleanString(model) && !/[\x00\r\n]/.test(model);
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }

function cleanString(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.trim() === value; }
