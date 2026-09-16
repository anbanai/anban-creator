import { createHash } from "node:crypto";
import { lstat, readFile, realpath } from "node:fs/promises";
import { dirname } from "node:path";
import { isAbsolute, posix, relative, resolve } from "node:path";

import type { JobConfig } from "./config.js";

const MAX_WORKLOAD_TOKEN_BYTES = 16 << 10;
const MAX_BOOTSTRAP_RESPONSE_BYTES = 2 << 20;
const MAX_BOOTSTRAP_FILE_BYTES = 64 << 20;
const MAX_BOOTSTRAP_FILES = 256;
const MAX_BOOTSTRAP_TURNS = 1000;
const MAX_BOOTSTRAP_PROMPT_BYTES = 1 << 20;
const MAX_EXECUTION_TOKEN_BYTES = 16 << 10;
const MAX_MODEL_USAGE_ALIASES = 128;
const MAX_CLAUDE_ENV_VALUE_BYTES = 16 << 10;
const MAX_CLAUDE_ENV_TOTAL_BYTES = 32 << 10;
const DEFAULT_AGENT_PACK_CATALOG_PATH = "/anbanai/agent-pack-catalog.json";
export const AGENT_RUNTIME_CONTRACT_VERSION = 2;

const BOOTSTRAP_RESPONSE_KEYS = [
  "execution_token", "execution_id", "task_id", "task_type", "project_id", "prompt",
  "agent_pack_id", "agent_pack_version", "agent_pack_digest", "runtime_profile", "runtime_adapter",
  "execution_profile", "max_turns", "agent_flag", "auto_memory_directory",
  "resume_session_id", "resume_context_path", "env", "files", "artifact_transport",
];

const EXECUTION_PROFILE_KEYS = [
  "profile_id", "provider", "protocol", "display_name", "profile_fingerprint", "envs", "model_usage_aliases",
];

const EXECUTION_PROFILE_IDS = new Set(["effective", "balanced", "quality"]);
export const CLAUDE_PROFILE_ENV_KEYS = new Set([
  "ANTHROPIC_AUTH_TOKEN",
  "ANTHROPIC_BASE_URL",
  "ANTHROPIC_MODEL",
  "ANTHROPIC_DEFAULT_OPUS_MODEL",
  "ANTHROPIC_DEFAULT_FABLE_MODEL",
  "ANTHROPIC_DEFAULT_SONNET_MODEL",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL",
  "CLAUDE_CODE_EFFORT_LEVEL",
  "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
  "CLAUDE_CODE_MAX_CONTEXT_TOKENS",
  "CLAUDE_CODE_MAX_OUTPUT_TOKENS",
  "MAX_THINKING_TOKENS",
  "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
  "CLAUDE_CODE_DISABLE_AUTO_MEMORY",
  "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
  "CLAUDE_CODE_DISABLE_THINKING",
  "CLAUDE_CODE_AUTO_COMPACT_WINDOW",
  "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE",
  "CLAUDE_CODE_DISABLE_1M_CONTEXT",
  "CLAUDE_CODE_SUBAGENT_MODEL",
  "ENABLE_TOOL_SEARCH",
]);

const REQUIRED_CLAUDE_PROFILE_ENV_KEYS = [
  "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
  "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
  "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
] as const;

const MODEL_CLAUDE_PROFILE_ENV_KEYS = [
  "ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
  "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
] as const;

export interface BootstrapFile {
  path: string;
  text?: string;
  download_url?: string;
  content_sha256?: string;
  mode: number;
  expected_size?: number;
  max_bytes?: number;
  replace_existing?: boolean;
}

const PUBLICATION_RECOVERY_REPLACE_PATHS = new Set([
  "output/04-article-final.md",
  "output/content-quality-report.md",
  "output/marketing-scan.json",
  "output/seo-result.md",
  "output/visual-rhythm-plan.md",
  "output/cover-plan.md",
  "output/cover-prompt.md",
  "output/image-plan.md",
  "output/05-article.html",
  "output/final-review.md",
  "output/viral-audit.md",
  "output/draft.json",
]);

export interface ExecutionProfile {
  profile_id: "effective" | "balanced" | "quality";
  provider: string;
  protocol: "anthropic";
  display_name: string;
  profile_fingerprint: string;
  envs: Record<string, string>;
  model_usage_aliases: Record<string, { provider: string; model: string }>;
}

export interface BootstrapResponse {
  execution_token: string;
  execution_id: string;
  task_id: string;
  task_type: string;
  agent_pack_id: string;
  agent_pack_version: string;
  agent_pack_digest: string;
  runtime_profile: string;
  runtime_adapter: "standard" | "openmontage";
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

export interface AgentPackProgressStage {
  id: string;
  title: string;
  active_percent: number;
  complete_percent: number;
  required_artifacts?: string[];
}

export interface AgentPack {
  id: string;
  version: string;
  digest: string;
  agent: { name: string };
  bindings: { task_types?: string[] };
  runtime: { profile?: string; adapter?: string; max_turns?: number };
  progress: AgentPackProgressStage[];
  progress_by_task_type?: Record<string, AgentPackProgressStage[]>;
}

export interface ResolvedBootstrapResponse extends BootstrapResponse {
  /** Runtime-only Pack snapshot selected from the validated Catalog. */
  resolved_agent_pack: AgentPack;
}

export interface AgentPackCatalog {
  packs: AgentPack[];
}

export function resolveAgentPackForTaskType(pack: AgentPack, taskType: string): AgentPack {
  const selected = pack.progress_by_task_type?.[taskType] ?? pack.progress;
  const progress = selected.map((stage) => {
    const cloned = { ...stage };
    if (stage.required_artifacts) cloned.required_artifacts = [...stage.required_artifacts];
    return cloned;
  });
  const { progress_by_task_type: _overrides, ...resolved } = pack;
  return { ...resolved, progress };
}

export interface BootstrapIdentity {
  execution_token: string;
  execution_id: string;
  task_id: string;
  project_id: string;
}

export class BootstrapResponseError extends Error {
  constructor(message: string, readonly identity: BootstrapIdentity, options?: ErrorOptions) {
    super(message, options);
    this.name = "BootstrapResponseError";
  }
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

export async function bootstrap(config: JobConfig, token: string, signal?: AbortSignal): Promise<ResolvedBootstrapResponse> {
  const response = await fetch(`${config.serverURL}/api/v1/agent/bootstrap`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", "X-Anban-Agent-Contract-Version": String(AGENT_RUNTIME_CONTRACT_VERSION) },
    body: JSON.stringify({ execution_id: config.executionID }),
    signal,
    redirect: "error",
  });
  const body = await readBoundedText(response, MAX_BOOTSTRAP_RESPONSE_BYTES, "bootstrap response");
  if (!response.ok) throw bootstrapHTTPError(response.status, body);
  let envelope: { code: number; msg?: string; data?: unknown };
  try {
    envelope = JSON.parse(body) as typeof envelope;
  } catch {
    throw new Error("bootstrap response is not valid JSON");
  }
  if (envelope.code !== 0 || !envelope.data) throw new Error("bootstrap request rejected");
  try {
    const data = validateBootstrapResponse(config.executionID, envelope.data);
    const pack = validateAgentPackCatalog(data, await readAgentPackCatalog());
    return { ...data, resolved_agent_pack: pack };
  } catch (error) {
    const identity = trustedBootstrapIdentity(config.executionID, envelope.data);
    if (identity) {
      throw new BootstrapResponseError(error instanceof Error ? error.message : "bootstrap response is invalid", identity, { cause: error });
    }
    throw error;
  }
}

function bootstrapHTTPError(status: number, body: string): Error {
  let code = "";
  let message = "";
  try {
    const envelope = JSON.parse(body) as { msg?: unknown; error_code?: unknown };
    if (typeof envelope.error_code === "string" && /^[a-z0-9_]{1,64}$/.test(envelope.error_code)) code = envelope.error_code;
    if (typeof envelope.msg === "string") message = envelope.msg.trim().replace(/\s+/g, " ").slice(0, 512);
  } catch {
    // Preserve the stable HTTP status when the server did not return JSON.
  }
  const detail = [code, message].filter(Boolean).join(": ");
  return new Error(`bootstrap returned HTTP ${status}${detail ? `: ${detail}` : ""}`);
}

export async function readAgentPackCatalog(path = DEFAULT_AGENT_PACK_CATALOG_PATH): Promise<AgentPackCatalog> {
  const info = await lstat(path);
  if (!info.isFile() || info.isSymbolicLink() || info.size > MAX_BOOTSTRAP_RESPONSE_BYTES) {
    throw new Error("Agent Pack Catalog is invalid");
  }
  let catalog: unknown;
  try {
    catalog = JSON.parse(await readFile(path, "utf8"));
  } catch {
    throw new Error("Agent Pack Catalog is not valid JSON");
  }
  validateAgentPackCatalogShape(catalog);
  return catalog;
}

function trustedBootstrapIdentity(executionID: string, input: unknown): BootstrapIdentity | undefined {
  if (!isRecord(input)) return undefined;
  const identity = {
    execution_token: input.execution_token,
    execution_id: input.execution_id,
    task_id: input.task_id,
    project_id: input.project_id,
  };
  if (!cleanString(identity.execution_token) || !cleanString(identity.execution_id) || !cleanString(identity.task_id) || !cleanString(identity.project_id)) return undefined;
  try {
    validateExecutionToken(executionID, identity as BootstrapResponse);
    return identity as BootstrapIdentity;
  } catch {
    return undefined;
  }
}

export function validateBootstrapResponse(executionID: string, input: unknown): BootstrapResponse {
  if (isRecord(input) && ["model", "runtime_env", "model_usage_aliases"].some((field) => field in input)) {
    throw new Error("bootstrap response contains legacy top-level model fields");
  }
  if (!isRecord(input) || !hasOnlyKeys(input, BOOTSTRAP_RESPONSE_KEYS)) {
    throw new Error("bootstrap response contains unknown fields");
  }
  const data = input as unknown as BootstrapResponse;
  if (!data || !cleanString(data.execution_token) || !cleanString(data.execution_id) || !cleanString(data.task_id) || !cleanString(data.project_id)) {
    throw new Error("bootstrap execution identity is incomplete");
  }
  if (data.execution_id !== executionID) throw new Error("bootstrap execution identity mismatch");
  validateExecutionToken(executionID, data);
  if (!cleanString(data.task_type) || !/^[a-z0-9]+(?:[-_][a-z0-9]+)*$/.test(data.task_type)) throw new Error("bootstrap task type is invalid");
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(data.agent_pack_id) || !/^\d+\.\d+\.\d+$/.test(data.agent_pack_version) || !/^[0-9a-f]{64}$/.test(data.agent_pack_digest)) throw new Error("bootstrap Agent Pack identity is invalid");
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(data.runtime_profile) || (data.runtime_adapter !== "standard" && data.runtime_adapter !== "openmontage")) throw new Error("bootstrap runtime identity is invalid");
  if (data.artifact_transport?.mode !== "direct" && data.artifact_transport?.mode !== "stream") throw new Error("bootstrap artifact transport is invalid");
  if (!cleanString(data.prompt) || Buffer.byteLength(data.prompt) > MAX_BOOTSTRAP_PROMPT_BYTES) throw new Error("bootstrap prompt is invalid");
  if (!Number.isInteger(data.max_turns) || data.max_turns < 1 || data.max_turns > MAX_BOOTSTRAP_TURNS) throw new Error("bootstrap max turns is invalid");
  if (!/^anban:[a-z0-9]+(?:-[a-z0-9]+)*$/.test(data.agent_flag)) throw new Error("bootstrap agent flag is invalid");
  if (data.auto_memory_directory !== ".claude/memory") throw new Error("bootstrap auto memory directory is invalid");
  validateExecutionProfile(data.execution_profile);
  if (data.resume_session_id && (!cleanString(data.resume_session_id) || data.resume_session_id.length > 128 || /[\s\x00-\x1f]/.test(data.resume_session_id))) throw new Error("bootstrap resume session ID is invalid");
  if (data.resume_session_id && !data.resume_context_path) throw new Error("bootstrap resume session requires resume context");
  if (data.resume_context_path !== undefined) {
    const expected = `.anban-creator/resume/executions/${executionID}/latest.md`;
    if (data.resume_context_path !== expected) throw new Error("bootstrap resume context path is invalid");
  }
  if (data.task_type !== "montage" && Object.keys(data.env ?? {}).length > 0) throw new Error("bootstrap environment is only valid for Montage tasks");
  for (const [key, value] of Object.entries(data.env ?? {})) validateEnvironmentEntry(key, value, "Montage");
  preflightBootstrapFiles(data.files ?? []);
  return data;
}

export function validateAgentPackCatalog(data: BootstrapResponse, catalog: AgentPackCatalog): AgentPackCatalog["packs"][number] {
  validateAgentPackCatalogShape(catalog);
  const matches = catalog.packs.filter((pack) => Array.isArray(pack.bindings?.task_types) && pack.bindings.task_types.includes(data.task_type));
  if (matches.length !== 1) throw new Error("bootstrap task type does not resolve to exactly one Agent Pack");
  const pack = matches[0]!;
  if (pack.id !== data.agent_pack_id || pack.version !== data.agent_pack_version || pack.digest !== data.agent_pack_digest || pack.runtime?.profile !== data.runtime_profile || pack.runtime?.adapter !== data.runtime_adapter || data.agent_flag !== `anban:${pack.agent?.name}`) {
    throw new Error("bootstrap Agent Pack identity does not match runtime Catalog");
  }
  return resolveAgentPackForTaskType(pack, data.task_type);
}

function validateAgentPackCatalogShape(input: unknown): asserts input is AgentPackCatalog {
  if (!isRecord(input) || !Array.isArray(input.packs)) throw new Error("Agent Pack Catalog is invalid");
  for (const rawPack of input.packs) {
    if (!isRecord(rawPack) || !Array.isArray(rawPack.progress) || rawPack.progress.length === 0) {
      throw new Error("Agent Pack Catalog progress is invalid");
    }
    validateProgressContract(rawPack.progress);
    if (rawPack.progress_by_task_type === undefined) continue;
    if (!isRecord(rawPack.progress_by_task_type) || !isRecord(rawPack.bindings) || !Array.isArray(rawPack.bindings.task_types)) {
      throw new Error("Agent Pack Catalog progress overrides are invalid");
    }
    const taskTypes = new Set(rawPack.bindings.task_types.filter((value): value is string => typeof value === "string"));
    for (const [taskType, progress] of Object.entries(rawPack.progress_by_task_type)) {
      if (!taskTypes.has(taskType) || !Array.isArray(progress) || progress.length === 0) {
        throw new Error("Agent Pack Catalog progress override is invalid");
      }
      validateProgressContract(progress);
    }
  }
}

function validateProgressContract(progress: unknown[]): void {
  const stageIDs = new Set<string>();
  let previousComplete = -1;
  for (const rawStage of progress) {
    if (!isRecord(rawStage)
      || !cleanString(rawStage.id)
      || !cleanString(rawStage.title)
      || !validProgressPercent(rawStage.active_percent)
      || !validProgressPercent(rawStage.complete_percent)
      || rawStage.active_percent > rawStage.complete_percent
      || (rawStage.required_artifacts !== undefined
        && (!Array.isArray(rawStage.required_artifacts)
          || !rawStage.required_artifacts.every((artifact) => cleanString(artifact) && validRequiredArtifactPath(artifact))))) {
      throw new Error("Agent Pack Catalog progress stage is invalid");
    }
    if (stageIDs.has(rawStage.id)) throw new Error("Agent Pack Catalog progress stage is duplicated");
    if (rawStage.complete_percent < previousComplete) throw new Error("Agent Pack Catalog progress complete_percent is decreasing");
    if (rawStage.active_percent < previousComplete) throw new Error("Agent Pack Catalog progress active_percent is decreasing");
    stageIDs.add(rawStage.id);
    previousComplete = rawStage.complete_percent;
  }
  if ((progress.at(-1) as Record<string, unknown>).complete_percent !== 100) {
    throw new Error("Agent Pack Catalog progress final complete_percent must be 100");
  }
}

function validProgressPercent(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= 0 && Number(value) <= 100;
}

function validRequiredArtifactPath(artifact: string): boolean {
  return !artifact.includes("\\")
    && !posix.isAbsolute(artifact)
    && !artifact.split("/").includes("..")
    && posix.normalize(artifact) === artifact
    && artifact !== "output"
    && artifact.startsWith("output/");
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
  const envs = validateClaudeProfileEnvs(profile.envs);
  const aliases = profile.model_usage_aliases;
  const requiredAliases = new Set(MODEL_CLAUDE_PROFILE_ENV_KEYS.flatMap((key) => envs[key] ? [envs[key]] : []));
  if (!isRecord(aliases) || Object.keys(aliases).length > MAX_MODEL_USAGE_ALIASES || ![...requiredAliases].every((raw) => isRecord(aliases[raw]))) throw new Error("bootstrap execution profile model usage aliases are invalid");
  if (!Object.entries(aliases).every(([raw, identity]) => isRecord(identity) && hasOnlyKeys(identity, ["provider", "model"]) && validModelUsageAlias(raw, identity.provider, identity.model) && identity.provider === profile.provider)) {
    throw new Error("bootstrap execution profile model usage aliases are invalid");
  }
  if (executionProfileFingerprint(profile as unknown as ExecutionProfile) !== profile.profile_fingerprint) {
    throw new Error("bootstrap execution profile fingerprint does not match snapshot");
  }
}

export function executionProfileFingerprint(profile: ExecutionProfile): string {
  const envs = Object.entries(profile.envs)
    .filter(([key]) => key !== "ANTHROPIC_AUTH_TOKEN")
    .map(([key, value]) => ({ key, value }))
    .sort((left, right) => compareCanonicalStrings(left.key, right.key) || compareCanonicalStrings(left.value, right.value));
  const modelUsageAliases = Object.entries(profile.model_usage_aliases)
    .map(([raw, identity]) => ({ raw, canonical: identity.model }))
    .sort((left, right) => compareCanonicalStrings(left.raw, right.raw) || compareCanonicalStrings(left.canonical, right.canonical));
  const canonical = goCompatibleJSON({
    schema_version: 3,
    profile_id: profile.profile_id,
    display_name: profile.display_name,
    provider: profile.provider,
    protocol: profile.protocol,
    envs,
    model_usage_aliases: modelUsageAliases,
  });
  return createHash("sha256").update(canonical).digest("hex");
}

function goCompatibleJSON(value: unknown): string {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, (character) => ({
    "<": "\\u003c",
    ">": "\\u003e",
    "&": "\\u0026",
    "\u2028": "\\u2028",
    "\u2029": "\\u2029",
  })[character]!);
}

function compareCanonicalStrings(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function validateClaudeProfileEnvs(input: unknown): Record<string, string> {
  if (!isRecord(input)) throw new Error("bootstrap execution profile environment is invalid");
  let totalBytes = 0;
  for (const [key, value] of Object.entries(input)) {
    if (!CLAUDE_PROFILE_ENV_KEYS.has(key) || !validClaudeEnvString(key, value) || /[\x00\r\n]/.test(value) || Buffer.byteLength(value) > MAX_CLAUDE_ENV_VALUE_BYTES) {
      throw new Error("bootstrap execution profile environment is invalid");
    }
    totalBytes += Buffer.byteLength(value);
    if (totalBytes > MAX_CLAUDE_ENV_TOTAL_BYTES) throw new Error("bootstrap execution profile environment is invalid");
  }
  const envs = input as Record<string, string>;
  if (!REQUIRED_CLAUDE_PROFILE_ENV_KEYS.every((key) => validClaudeEnvString(key, envs[key]))) throw new Error("bootstrap execution profile environment is invalid");
  if (!validProviderBaseURL(envs.ANTHROPIC_BASE_URL)) throw new Error("bootstrap execution profile environment is invalid");
  if (envs.CLAUDE_CODE_EFFORT_LEVEL !== undefined && !new Set(["low", "medium", "high", "max"]).has(envs.CLAUDE_CODE_EFFORT_LEVEL)) throw new Error("bootstrap execution profile environment is invalid");
  for (const key of ["CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING", "CLAUDE_CODE_DISABLE_THINKING", "CLAUDE_CODE_DISABLE_1M_CONTEXT", "ENABLE_TOOL_SEARCH"]) {
    if (envs[key] !== undefined && envs[key] !== "true" && envs[key] !== "false") throw new Error("bootstrap execution profile environment is invalid");
  }
  if (envs.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC !== undefined && envs.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC !== "0" && envs.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC !== "1") throw new Error("bootstrap execution profile environment is invalid");
  if (envs.CLAUDE_CODE_DISABLE_AUTO_MEMORY !== undefined && envs.CLAUDE_CODE_DISABLE_AUTO_MEMORY !== "0") throw new Error("bootstrap execution profile environment is invalid");
  for (const key of ["CLAUDE_CODE_MAX_CONTEXT_TOKENS", "CLAUDE_CODE_MAX_OUTPUT_TOKENS", "CLAUDE_CODE_AUTO_COMPACT_WINDOW"]) {
    if (envs[key] !== undefined && !validUnsignedInteger(envs[key], false)) throw new Error("bootstrap execution profile environment is invalid");
  }
  if (envs.MAX_THINKING_TOKENS !== undefined && !validUnsignedInteger(envs.MAX_THINKING_TOKENS, true)) throw new Error("bootstrap execution profile environment is invalid");
  if (envs.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE !== undefined && (!validUnsignedInteger(envs.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE, false) || BigInt(envs.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE) > 100n)) throw new Error("bootstrap execution profile environment is invalid");
  return envs;
}

function validClaudeEnvString(key: string, value: unknown): value is string {
  if (typeof value !== "string" || value.trim().length === 0) return false;
  return key === "ANTHROPIC_AUTH_TOKEN" || value.trim() === value;
}

function validUnsignedInteger(value: string, allowZero: boolean): boolean {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) return false;
  try {
    const parsed = BigInt(value);
    return (allowZero ? parsed >= 0n : parsed > 0n) && parsed <= 9223372036854775807n;
  } catch {
    return false;
  }
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: string[]): boolean {
  const keys = Object.keys(value);
  return keys.length <= allowed.length && keys.every((key) => allowed.includes(key));
}

export function preflightBootstrapFiles(files: BootstrapFile[]): BootstrapFile[] {
  if (files.length > MAX_BOOTSTRAP_FILES) throw new Error("bootstrap file count exceeds limit");
  const seen = new Map<string, string>();
  for (const file of files) {
    if (!isRecord(file) || !hasOnlyKeys(file, [
      "path", "text", "download_url", "mode", "expected_size", "max_bytes", "replace_existing",
      "content_sha256",
    ])) throw new Error("bootstrap file contains unknown fields");
    if (file.replace_existing !== undefined && typeof file.replace_existing !== "boolean") {
      throw new Error("bootstrap file replace_existing must be boolean");
    }
    const relative = cleanBootstrapPath(file.path);
    const key = relative.toLowerCase();
    if (seen.has(key)) throw new Error(`duplicate bootstrap path ${relative} conflicts with ${seen.get(key)}`);
    if (key === ".claude/memory" || key.startsWith(".claude/memory/")) throw new Error(`bootstrap path ${relative} targets protected auto memory`);
    seen.set(key, relative);
    const inline = file.text !== undefined;
    const remote = cleanString(file.download_url ?? "");
    if (inline === remote) throw new Error(`bootstrap file ${relative} must have exactly one content source`);
    if (file.content_sha256 !== undefined && !/^[0-9a-f]{64}$/.test(file.content_sha256)) throw new Error(`bootstrap file ${relative} has invalid content SHA-256`);
    if (file.replace_existing && key !== ".anban-creator/settings.json"
      && (!PUBLICATION_RECOVERY_REPLACE_PATHS.has(key) || !remote || !file.content_sha256)) {
      throw new Error(`bootstrap file ${relative} cannot replace existing workspace content`);
    }
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
  if (components.some((part) => !part || part === "." || part === ".." || Buffer.byteLength(part) > 255 || /[\x00-\x1f<>:"\\|?*]/.test(part) || /[. ]$/.test(part) || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(part))) throw new Error(`bootstrap path ${raw} escapes workspace or is not clean`);
  return raw;
}

export function workspacePath(workspace: string, relativePath: string): string {
  const clean = cleanBootstrapPath(relativePath);
  const root = resolve(workspace);
  const target = resolve(root, clean);
  const rel = relative(root, target);
  if (rel === ".." || rel.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`) || isAbsolute(rel)) throw new Error(`unsafe workspace path ${relativePath}`);
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
  if (data.execution_id !== executionID) throw new Error("bootstrap response identity mismatch");
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
