import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";

import { uploadWorkspaceArtifacts } from "./artifacts.js";
import { readAgentPackCatalog, resolveAgentPackForTaskType, type AgentPack, type AgentPackCatalog, type BootstrapResponse, type ResolvedBootstrapResponse } from "./bootstrap.js";
import { CompletionReportError } from "./errors.js";
import { evaluateCompletionMetadata } from "./completion-evaluator.js";
import { Reporter, type ExecutionResult } from "./reporter.js";
import { appendResumeContextToPrompt } from "./resume.js";
import { runClaude } from "./runner.js";
import { prepareWorkspace } from "./workspace.js";

const HEARTBEAT_INTERVAL_MS = 30_000;

export interface LocalConfig {
  serverURL: string;
  executionToken: string;
  taskID: string;
  executionID: string;
  taskType: string;
  agentPackID: string;
  agentPackVersion: string;
  agentPackDigest: string;
  runtimeAdapter: "standard" | "openmontage";
  runtimeProfile: string;
  topic: string;
  workspace: string;
  agentFlag: string;
  maxTurns: number;
  model?: string;
  autoMemoryDirectory?: string;
  artifactUploadMode: "direct" | "stream";
  hasContentImage: boolean;
  hasTailImage: boolean;
  articleWithCover: boolean;
  articleWithContentImages: boolean;
  modelUsageAliases: BootstrapResponse["execution_profile"]["model_usage_aliases"];
  agentPack: AgentPack;
}

export async function parseLocalConfig(args: string[], env: NodeJS.ProcessEnv = process.env): Promise<LocalConfig> {
  if (args[0] !== "run") throw new Error("run subcommand is required");
  const values = new Map<string, string>();
  const booleans = new Map<string, boolean>();
  const aliases: string[] = [];
  for (let index = 1; index < args.length; index += 1) {
    const raw = args[index]!;
    const equals = raw.indexOf("=");
    const flag = equals >= 0 ? raw.slice(0, equals) : raw;
    if (["--has-content-image", "--has-tail-image", "--article-with-cover", "--article-with-content-images"].includes(flag)) {
      const value = equals >= 0 ? raw.slice(equals + 1) : "true";
      if (value !== "true" && value !== "false") throw new Error(`${flag} must be true or false`);
      booleans.set(flag, value === "true");
      continue;
    }
    if (equals >= 0) throw new Error(`unknown argument ${raw}`);
    const value = args[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`${flag} is required`);
    index += 1;
    if (flag === "--model-usage-alias") aliases.push(value);
    else if (["--server-url", "--task-id", "--execution-id", "--task-type", "--agent-pack-id", "--agent-pack-version", "--agent-pack-digest", "--runtime-adapter", "--runtime-profile", "--topic", "--workspace", "--agent-flag", "--auto-memory-directory", "--artifact-upload-mode", "--max-turns", "--model"].includes(flag)) values.set(flag, value.trim());
    else throw new Error(`unknown argument ${flag}`);
  }

  const required = (flag: string): string => {
    const value = values.get(flag)?.trim();
    if (!value) throw new Error(`${flag} is required`);
    return value;
  };
  const taskType = required("--task-type");
  const pluginRoot = resolve(env.CLAUDE_PLUGIN_ROOT || "/anbanai");
  const catalog = await readAgentPackCatalog(join(pluginRoot, "agent-pack-catalog.json"));
  const catalogPack = resolveLocalPack(catalog, taskType);
  const providedIdentity = ["--agent-pack-id", "--agent-pack-version", "--agent-pack-digest", "--runtime-adapter", "--runtime-profile"].some((flag) => values.has(flag));
  if (providedIdentity && (
    values.get("--agent-pack-id") !== catalogPack.id
    || values.get("--agent-pack-version") !== catalogPack.version
    || values.get("--agent-pack-digest") !== catalogPack.digest
    || values.get("--runtime-adapter") !== catalogPack.runtime.adapter
    || values.get("--runtime-profile") !== catalogPack.runtime.profile
  )) throw new Error("frozen Agent Pack identity does not match runtime Catalog");
  const pack = resolveAgentPackForTaskType(catalogPack, taskType);

  const server = new URL(required("--server-url"));
  if (!["http:", "https:"].includes(server.protocol) || server.username || server.password || server.search || server.hash || (server.pathname && server.pathname !== "/")) throw new Error("server URL is invalid");
  const maxTurns = values.has("--max-turns") ? Number(values.get("--max-turns")) : pack.runtime.max_turns ?? 40;
  if (!Number.isInteger(maxTurns) || maxTurns < 1) throw new Error("--max-turns must be a positive integer");
  const artifactUploadMode = required("--artifact-upload-mode").toLowerCase();
  if (artifactUploadMode !== "direct" && artifactUploadMode !== "stream") throw new Error("--artifact-upload-mode must be one of: direct, stream");
  const runtimeAdapter = pack.runtime.adapter;
  if (runtimeAdapter !== "standard" && runtimeAdapter !== "openmontage") throw new Error("Agent Pack runtime adapter is invalid");

  return {
    serverURL: server.origin,
    executionToken: env.ANBAN_EXECUTION_TOKEN?.trim() || (() => { throw new Error("ANBAN_EXECUTION_TOKEN is required"); })(),
    taskID: required("--task-id"),
    executionID: required("--execution-id"),
    taskType,
    agentPackID: pack.id,
    agentPackVersion: pack.version,
    agentPackDigest: pack.digest,
    runtimeAdapter,
    runtimeProfile: pack.runtime.profile!,
    topic: values.get("--topic") ?? "",
    workspace: resolve(values.get("--workspace") || "/workspace"),
    agentFlag: values.get("--agent-flag") || `anban:${pack.agent.name}`,
    maxTurns,
    model: values.get("--model") || undefined,
    autoMemoryDirectory: values.get("--auto-memory-directory") || undefined,
    artifactUploadMode,
    hasContentImage: booleans.get("--has-content-image") ?? true,
    hasTailImage: booleans.get("--has-tail-image") ?? false,
    articleWithCover: booleans.get("--article-with-cover") ?? true,
    articleWithContentImages: booleans.get("--article-with-content-images") ?? true,
    modelUsageAliases: parseModelUsageAliases(aliases),
    agentPack: pack,
  };
}

export async function runLocal(
  args: string[],
  stdout: NodeJS.WritableStream = process.stdout,
  stderr: NodeJS.WritableStream = process.stderr,
): Promise<ExecutionResult> {
  const config = await parseLocalConfig(args);
  await prepareWorkspace(config.workspace, config.taskType, config.runtimeAdapter);
  const data = localBootstrap(config);
  const reporter = createLocalReporter(config);
  const shutdown = new AbortController();
  const onShutdown = () => shutdown.abort(new Error("agent shutdown: received termination signal"));
  process.once("SIGINT", onShutdown);
  process.once("SIGTERM", onShutdown);
  const stopHeartbeat = startHeartbeat(reporter, stderr, shutdown.signal);
  try {
    let result = await runClaude(config.workspace, data, config.serverURL, config.executionToken, reporter, shutdown.signal);
    try {
      await uploadWorkspaceArtifacts(config.workspace, { ...data, artifact_transport: { mode: config.artifactUploadMode } }, reporter, shutdown.signal);
    } catch (error) {
      const message = error instanceof Error ? error.message : "artifact upload failed";
      void reporter.progress(`artifact upload failed: ${message}`, shutdown.signal).catch(() => {});
      if (result.success) result = { ...result, success: false, terminal_reason: "platform_error", error: `artifact upload failed: ${message}` };
    }
    let completionError: Error | undefined;
    try {
      try {
        await evaluateCompletionMetadata(config.workspace, data.execution_profile, shutdown.signal);
        const metadata = JSON.parse(await readFile(join(config.workspace, "output", "completion-metadata.json"), "utf8"));
        await reporter.submitCompletionMetadata(metadata, shutdown.signal);
      } catch (metadataError) {
        stderr.write(`completion metadata upload skipped: ${(metadataError as Error).message}\n`);
      }
      await reporter.complete(result);
    } catch (error) {
      completionError = error instanceof Error ? error : new Error("completion report failed");
    }
    stdout.write(`${JSON.stringify(result)}\n`);
    if (completionError) throw new CompletionReportError(completionError);
    return result;
  } finally {
    stopHeartbeat();
    process.removeListener("SIGINT", onShutdown);
    process.removeListener("SIGTERM", onShutdown);
  }
}

export function createLocalReporter(config: Pick<LocalConfig, "serverURL" | "executionID" | "executionToken" | "taskID">): Reporter {
  return new Reporter({ serverURL: config.serverURL, executionID: config.executionID }, config.executionToken, config.taskID);
}

function localBootstrap(config: LocalConfig): ResolvedBootstrapResponse {
  const inheritedKey = process.env.ANTHROPIC_API_KEY?.trim();
  const envs: Record<string, string> = {};
  if (inheritedKey) envs.ANTHROPIC_API_KEY = inheritedKey;
  if (config.model) envs.ANTHROPIC_MODEL = config.model;
  return {
    execution_token: config.executionToken,
    execution_id: config.executionID,
    task_id: config.taskID,
    task_type: config.taskType,
    project_id: process.env.ANBAN_DEFAULT_PROJECT?.trim() || "",
    prompt: buildLocalPrompt(config, process.env.ANBAN_DEFAULT_PROJECT?.trim() || ""),
    agent_pack_id: config.agentPackID,
    agent_pack_version: config.agentPackVersion,
    agent_pack_digest: config.agentPackDigest,
    runtime_profile: config.runtimeProfile,
    runtime_adapter: config.runtimeAdapter,
    execution_profile: {
      profile_id: "effective",
      provider: "local",
      protocol: "anthropic",
      display_name: "Local",
      profile_fingerprint: "0".repeat(64),
      envs,
      model_usage_aliases: config.modelUsageAliases,
    },
    max_turns: config.maxTurns,
    agent_flag: config.agentFlag,
    auto_memory_directory: config.autoMemoryDirectory,
    artifact_transport: { mode: "stream" },
    resolved_agent_pack: config.agentPack,
  };
}

export function buildLocalPrompt(config: Pick<LocalConfig, "taskType" | "topic" | "taskID" | "hasContentImage" | "hasTailImage" | "articleWithCover" | "articleWithContentImages" | "workspace">, projectID: string): string {
  let prompt = config.topic
    ? `Run the full ${config.taskType} creation workflow; create content about: ${config.topic}`
    : `Run the full ${config.taskType} creation workflow. Analyze the project profile, keywords, and historical topics to choose the optimal theme, then execute the full creation workflow.`;
  if (config.taskType === "seednote") prompt += `\n\n运行控制：\n- seednote_image_mode=${seednoteImageMode(config.hasContentImage, config.hasTailImage)}`;
  if (config.taskType === "article") prompt += `\n\n运行控制：\n- article_image_mode=${articleImageMode(config.articleWithCover, config.articleWithContentImages)}`;
  const context = [`task_id=${config.taskID}`, ...(projectID ? [`project_id=${projectID}`] : [])];
  prompt += `\n\n本任务上下文：${context.join(", ")}`;
  return appendResumeContext(prompt, config.workspace);
}

function appendResumeContext(prompt: string, workspace: string): string {
  const relative = ".anban-creator/resume/latest.md";
  if (!existsSync(join(workspace, relative))) return prompt;
  return appendResumeContextToPrompt(prompt, relative);
}

function seednoteImageMode(content: boolean, tail: boolean): string {
  if (content && tail) return "full";
  if (content) return "cover_content";
  if (tail) return "cover_tail";
  return "cover_only";
}

function articleImageMode(cover: boolean, content: boolean): string {
  if (cover && content) return "cover_and_content";
  if (cover) return "cover_only";
  if (content) return "content_only";
  return "text_only";
}

function resolveLocalPack(catalog: AgentPackCatalog, taskType: string): AgentPackCatalog["packs"][number] {
  const matches = catalog.packs.filter((pack) => pack.bindings.task_types?.includes(taskType));
  if (matches.length !== 1 || !matches[0]!.runtime.profile || !matches[0]!.runtime.adapter) throw new Error(`task-type ${JSON.stringify(taskType)} has no Agent Pack`);
  return matches[0]!;
}

function parseModelUsageAliases(values: string[]): BootstrapResponse["execution_profile"]["model_usage_aliases"] {
  const aliases: BootstrapResponse["execution_profile"]["model_usage_aliases"] = {};
  for (const value of values) {
    const equals = value.indexOf("=");
    const slash = value.indexOf("/", equals + 1);
    if (equals < 1 || slash <= equals + 1 || slash === value.length - 1) throw new Error(`invalid model usage alias ${JSON.stringify(value)}`);
    aliases[value.slice(0, equals)] = { provider: value.slice(equals + 1, slash), model: value.slice(slash + 1) };
  }
  return aliases;
}

function startHeartbeat(reporter: Pick<Reporter, "heartbeat">, stderr: NodeJS.WritableStream, signal: AbortSignal): () => void {
  let failures = 0;
  const report = async () => {
    try { await reporter.heartbeat(signal); failures = 0; }
    catch (error) { failures += 1; if (failures === 1 || failures % 5 === 0) stderr.write(`agent heartbeat failed (${failures} consecutive): ${(error as Error).message}\n`); }
  };
  void report();
  const interval = setInterval(() => void report(), HEARTBEAT_INTERVAL_MS);
  return () => clearInterval(interval);
}
