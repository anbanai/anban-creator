import { spawn } from "node:child_process";

import { query, type HookJSONOutput, type ModelUsage, type Options, type SDKMessage, type SDKSystemMessage } from "@anthropic-ai/claude-agent-sdk";

import { CLAUDE_PROFILE_ENV_KEYS, type BootstrapResponse } from "./bootstrap.js";
import { collectGeneratedImageDescriptors, materializeGeneratedImage } from "./downloads.js";
import type { ExecutionResult, Reporter } from "./reporter.js";

const allowedTools = ["Read", "Write", "Edit", "Glob", "Grep", "Bash", "Skill", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TaskOutput", "TaskStop", "TodoWrite", "WebSearch", "WebFetch", "NotebookEdit", "mcp__anban__*"];
const disallowedTools = ["Agent", "ScheduleWakeup", "AskUserQuestion"];
// Keep this list aligned with server/agent/claude_runtime_env.go and the
// authentication, provider-routing, and model inputs in SDK 0.3.220.
const CLAUDE_INHERITED_ENVIRONMENT_KEYS_TO_UNSET = [
  "AGENT_PROXY_AUTH_TOKEN",
  "AGENT_PROXY_URL",
  "ANTHROPIC_API_KEY",
  "ANTHROPIC_AWS_API_KEY",
  "ANTHROPIC_AWS_BASE_URL",
  "ANTHROPIC_AWS_WORKSPACE_ID",
  "ANTHROPIC_BEDROCK_BASE_URL",
  "ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
  "ANTHROPIC_BEDROCK_SERVICE_TIER",
  "ANTHROPIC_CONFIG_DIR",
  "ANTHROPIC_CUSTOM_HEADERS",
  "ANTHROPIC_DEFAULT_FABLE_MODEL_DESCRIPTION",
  "ANTHROPIC_DEFAULT_FABLE_MODEL_NAME",
  "ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL_DESCRIPTION",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES",
  "ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION",
  "ANTHROPIC_DEFAULT_OPUS_MODEL_NAME",
  "ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES",
  "ANTHROPIC_DEFAULT_SONNET_MODEL_DESCRIPTION",
  "ANTHROPIC_DEFAULT_SONNET_MODEL_NAME",
  "ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES",
  "ANTHROPIC_FEDERATION_RULE_ID",
  "ANTHROPIC_FOUNDRY_API_KEY",
  "ANTHROPIC_FOUNDRY_AUTH_TOKEN",
  "ANTHROPIC_FOUNDRY_BASE_URL",
  "ANTHROPIC_FOUNDRY_RESOURCE",
  "ANTHROPIC_GOOGLE_CLOUD_BASE_URL",
  "ANTHROPIC_GOOGLE_CLOUD_LOCATION",
  "ANTHROPIC_GOOGLE_CLOUD_PROJECT",
  "ANTHROPIC_GOOGLE_CLOUD_WORKSPACE_ID",
  "ANTHROPIC_IDENTITY_TOKEN",
  "ANTHROPIC_IDENTITY_TOKEN_FILE",
  "ANTHROPIC_ORGANIZATION_ID",
  "ANTHROPIC_PROFILE",
  "ANTHROPIC_SCOPE",
  "ANTHROPIC_SERVICE_ACCOUNT_ID",
  "ANTHROPIC_SMALL_FAST_MODEL",
  "ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION",
  "ANTHROPIC_UNIX_SOCKET",
  "ANTHROPIC_VERTEX_BASE_URL",
  "ANTHROPIC_VERTEX_PROJECT_ID",
  "ANTHROPIC_WORKSPACE_ID",
  "AWS_ACCESS_KEY_ID",
  "AWS_BEARER_TOKEN_BEDROCK",
  "AWS_CONFIG_FILE",
  "AWS_CONTAINER_AUTHORIZATION_TOKEN",
  "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
  "AWS_CONTAINER_CREDENTIALS_FULL_URI",
  "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
  "AWS_DEFAULT_REGION",
  "AWS_EC2_METADATA_SERVICE_ENDPOINT",
  "AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE",
  "AWS_PROFILE",
  "AWS_REGION",
  "AWS_ROLE_ARN",
  "AWS_SECRET_ACCESS_KEY",
  "AWS_SESSION_TOKEN",
  "AWS_SHARED_CREDENTIALS_FILE",
  "AWS_WEB_IDENTITY_TOKEN_FILE",
  "CLAUDE_CODE_3P_PROBE_WROTE_OPUS_DEFAULT",
  "CLAUDE_CODE_3P_PROBE_WROTE_SONNET_DEFAULT",
  "CLAUDE_CODE_ACCOUNT_TAGGED_ID",
  "CLAUDE_CODE_ACCOUNT_UUID",
  "CLAUDE_CODE_API_BASE_URL",
  "CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR",
  "CLAUDE_CODE_ARTIFACTS_API_BASE_URL",
  "CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL",
  "CLAUDE_CODE_CERT_STORE",
  "CLAUDE_CODE_CLIENT_CERT",
  "CLAUDE_CODE_CLIENT_KEY",
  "CLAUDE_CODE_CLIENT_KEY_PASSPHRASE",
  "CLAUDE_CODE_CUSTOM_OAUTH_URL",
  "CLAUDE_CODE_DESIGN_OAUTH_CLIENT_ID",
  "CLAUDE_CODE_ENABLE_PROXY_AUTH_HELPER",
  "CLAUDE_CODE_HFI_BEARER_TOKEN",
  "CLAUDE_CODE_HOST_AUTH_ENV_VAR",
  "CLAUDE_CODE_HOST_AUTH_REFRESH_TIMEOUT_MS",
  "CLAUDE_CODE_HOST_CREDS_FILE",
  "CLAUDE_CODE_MOCK_REMOTE_SETTINGS",
  "CLAUDE_CODE_OAUTH_CLIENT_ID",
  "CLAUDE_CODE_OAUTH_REFRESH_TOKEN",
  "CLAUDE_CODE_OAUTH_SCOPES",
  "CLAUDE_CODE_OAUTH_TOKEN",
  "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR",
  "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST",
  "CLAUDE_CODE_PROXY_AUTH_HELPER_TTL_MS",
  "CLAUDE_CODE_PROXY_RESOLVES_HOSTS",
  "CLAUDE_CODE_REMOTE_SETTINGS_PATH",
  "CLAUDE_CODE_REMOTE_SETTINGS_POLL_MS",
  "CLAUDE_CODE_SDK_HAS_HOST_AUTH_REFRESH",
  "CLAUDE_CODE_SDK_HAS_OAUTH_REFRESH",
  "CLAUDE_CODE_SESSION_ACCESS_TOKEN",
  "CLAUDE_CODE_SKIP_ANTHROPIC_AWS_AUTH",
  "CLAUDE_CODE_SKIP_ANTHROPIC_GOOGLE_CLOUD_AUTH",
  "CLAUDE_CODE_SKIP_AWS_CRED_CACHE",
  "CLAUDE_CODE_SKIP_BEDROCK_AUTH",
  "CLAUDE_CODE_SKIP_FOUNDRY_AUTH",
  "CLAUDE_CODE_SKIP_MANTLE_AUTH",
  "CLAUDE_CODE_SKIP_VERTEX_AUTH",
  "CLAUDE_CODE_USE_ANTHROPIC_AWS",
  "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
  "CLAUDE_CODE_USE_BEDROCK",
  "CLAUDE_CODE_USE_FOUNDRY",
  "CLAUDE_CODE_USE_GATEWAY",
  "CLAUDE_CODE_USE_MANTLE",
  "CLAUDE_CODE_USE_VERTEX",
  "CLAUDE_CODE_WEBSOCKET_AUTH_FILE_DESCRIPTOR",
  "CLAUDE_CONFIG_DIR",
  "CLAUDE_LOCAL_OAUTH_API_BASE",
  "CLAUDE_LOCAL_OAUTH_APPS_BASE",
  "CLAUDE_LOCAL_OAUTH_CONSOLE_BASE",
  "CLAUDE_SESSION_INGRESS_TOKEN_FILE",
  "CLAUDE_SECURESTORAGE_CONFIG_DIR",
  "CLAUDE_TRUSTED_DEVICE_TOKEN",
  "CLOUD_ML_REGION",
  "CLOUDSDK_AUTH_ACCESS_TOKEN",
  "ENVIRONMENT_SERVICE_KEY",
  "GCE_METADATA_HOST",
  "GCE_METADATA_IP",
  "GCLOUD_PROJECT",
  "GOOGLE_APPLICATION_CREDENTIALS",
  "GOOGLE_CLOUD_PROJECT",
  "GOOGLE_CLOUD_QUOTA_PROJECT",
  "METADATA_SERVER_DETECTION",
  "USE_LOCAL_OAUTH",
  "USE_STAGING_OAUTH",
  "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL",
] as const;
const CLAUDE_ENVIRONMENT_KEYS_TO_UNSET = new Set([
  ...CLAUDE_PROFILE_ENV_KEYS,
  ...CLAUDE_INHERITED_ENVIRONMENT_KEYS_TO_UNSET,
]);

interface TrackedToolCall { name: string; input: Record<string, unknown>; }

export async function runClaude(workspace: string, data: BootstrapResponse, serverURL: string, token: string, reporter: Reporter, signal: AbortSignal): Promise<ExecutionResult> {
  const controller = new AbortController();
  signal.addEventListener("abort", () => controller.abort(), { once: true });
  const options = buildQueryOptions(data, workspace, serverURL, token, reporter, controller);
  const cwd = options.cwd!;
  let logText = "";
  let initValidated = false;
  const toolCalls = new Map<string, TrackedToolCall>();
  for await (const message of query({ prompt: data.prompt, options })) {
    const consumed = await consumeMessage(message, reporter, logText, toolCalls, cwd, serverURL, token, controller.signal, data.task_type, data.execution_profile.model_usage_aliases);
    logText = consumed.logText;
    if (message.type === "system" && message.subtype === "init") {
      validateManagedInit(message, data.task_type);
      initValidated = true;
    }
    if (consumed.terminal) {
      if (!consumed.terminal.success && !consumed.terminal.error) consumed.terminal.error = `agent execution failed (subtype=${consumed.terminal.result_subtype ?? "unknown"})`;
      if (consumed.terminal.success && !initValidated) consumed.terminal = { ...consumed.terminal, success: false, error: "managed plugin readiness failed: Claude Code did not emit system/init" };
      return consumed.terminal;
    }
  }
  return { success: false, error: "managed agent stream ended without a result message", work_dir: workspace, log_text: logText };
}

export function buildQueryOptions(
  data: BootstrapResponse,
  workspace: string,
  serverURL = "https://server.invalid",
  token = "test-token",
  reporter: Pick<Reporter, "progress"> = { progress: async () => {} },
  controller = new AbortController(),
): Options {
  const cwd = data.task_type === "montage" ? `${workspace}/openmontage` : workspace;
  return {
    abortController: controller,
    cwd,
    maxTurns: data.max_turns,
    agent: data.agent_flag,
    resume: data.resume_session_id,
    permissionMode: "dontAsk",
    allowedTools,
    disallowedTools,
    canUseTool: async (toolName) => isAllowedTool(toolName) ? { behavior: "allow", updatedInput: undefined } : { behavior: "deny", message: `tool ${toolName} is outside the managed Agent SDK allowlist` },
    plugins: [{ type: "local", path: "/anbanai", skipMcpDiscovery: true }],
    mcpServers: { anban: { type: "http", url: `${serverURL}/mcp`, headers: { Authorization: `Bearer ${token}` }, timeout: 900000 } },
    strictMcpConfig: true,
    env: buildExecutionEnvironment(process.env, data, serverURL, token),
    includePartialMessages: false,
    stderr: (line) => void reporter.progress(line.trim()),
    hooks: data.task_type === "seednote" ? { Stop: [{ hooks: [createSeednoteStopGate(cwd)] }] } : undefined,
  };
}

export function buildExecutionEnvironment(
  processEnvironment: NodeJS.ProcessEnv,
  data: Pick<BootstrapResponse, "task_type" | "env" | "project_id" | "execution_profile">,
  serverURL: string,
  token: string,
): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = { ...processEnvironment, ...(data.env ?? {}) };
  for (const key of CLAUDE_ENVIRONMENT_KEYS_TO_UNSET) delete environment[key];
  return {
    ...environment,
    ANBAN_API_KEY: token,
    ANBAN_API_URL: serverURL,
    ANBAN_DEFAULT_PROJECT: data.project_id,
    ...data.execution_profile.envs,
  };
}

export function validateManagedInit(message: Pick<SDKSystemMessage, "type" | "subtype" | "mcp_servers" | "plugins" | "skills">, taskType: string): void {
  const managed = message.mcp_servers.find((server) => server.name === "anban");
  if (!managed || managed.status !== "connected") throw new Error(`managed MCP readiness failed: server "anban" is not connected`);
  if (!message.plugins.some((plugin) => plugin.name === "anban")) throw new Error("managed plugin readiness failed: plugin anban is not loaded");
  const skills = new Set(message.skills);
  for (const required of requiredSkills(taskType)) if (!skills.has(required)) throw new Error(`managed plugin readiness failed: skill ${required} is not loaded`);
}

async function consumeMessage(message: SDKMessage, reporter: Reporter, logText: string, toolCalls: Map<string, TrackedToolCall>, cwd: string, serverURL: string, token: string, signal: AbortSignal, _taskType: string, aliases: BootstrapResponse["execution_profile"]["model_usage_aliases"]): Promise<{ logText: string; terminal?: ExecutionResult }> {
  if (message.type === "assistant") {
    for (const block of message.message.content) {
      if (block.type === "text" && block.text.trim()) {
        logText = logText ? `${logText}\n${block.text.trim()}` : block.text.trim();
        void reporter.progress(block.text.trim());
      }
      if (block.type === "tool_use") {
        toolCalls.set(block.id, { name: block.name, input: block.input as Record<string, unknown> });
        void reporter.progress(`Using tool: ${block.name}`);
      }
    }
  }
  if (message.type === "user") await handleToolResults(message as unknown as { message: { content?: unknown } }, toolCalls, cwd, serverURL, token, signal);
  if (message.type === "result") {
    const error = message.subtype === "success" ? undefined : message.errors.join("\n");
    const usage = terminalModelUsage(message.modelUsage ?? {}, aliases);
    return { logText, terminal: { success: message.subtype === "success", error, work_dir: cwd, session_id: message.session_id, result_subtype: message.subtype, num_turns: message.num_turns, duration_ms: message.duration_ms, duration_api_ms: message.duration_api_ms, log_text: logText, model_usage: usage.usage, cost_status: usage.cost_status, cost_diagnostics: usage.cost_diagnostics } };
  }
  return { logText };
}

export function terminalModelUsage(modelUsage: Record<string, unknown>, aliases: BootstrapResponse["execution_profile"]["model_usage_aliases"]): { usage: Array<Record<string, string | number>>; cost_status: "reconciled" | "unreconciled"; cost_diagnostics: Array<{ code: string; raw_model?: string }> } {
  const usage: Array<Record<string, string | number>> = [];
  const diagnostics: Array<{ code: string; raw_model?: string }> = [];
  for (const raw of Object.keys(modelUsage).sort()) {
    const tokens = modelUsage[raw];
    if (!isModelUsageTokens(tokens)) {
      diagnostics.push({ code: "invalid_model_usage_tokens", raw_model: raw });
      continue;
    }
    const identity = aliases[raw];
    if (!identity?.provider || !identity.model) {
      diagnostics.push({ code: "unmapped_model_usage_alias", raw_model: raw });
      continue;
    }
    usage.push({ provider: identity.provider, model: identity.model, input_tokens: tokens.inputTokens, output_tokens: tokens.outputTokens, cache_read_input_tokens: tokens.cacheReadInputTokens, cache_creation_input_tokens: tokens.cacheCreationInputTokens });
  }
  usage.sort((left, right) => `${left.provider}/${left.model}`.localeCompare(`${right.provider}/${right.model}`));
  if (Object.keys(modelUsage).length === 0) diagnostics.push({ code: "missing_terminal_model_usage" });
  return { usage, cost_status: diagnostics.length ? "unreconciled" : "reconciled", cost_diagnostics: diagnostics };
}

function isModelUsageTokens(value: unknown): value is Pick<ModelUsage, "inputTokens" | "outputTokens" | "cacheReadInputTokens" | "cacheCreationInputTokens"> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const tokens = value as Record<string, unknown>;
  return [tokens.inputTokens, tokens.outputTokens, tokens.cacheReadInputTokens, tokens.cacheCreationInputTokens]
    .every((token) => Number.isSafeInteger(token) && (token as number) >= 0);
}

async function handleToolResults(message: { message: { content?: unknown } }, toolCalls: Map<string, TrackedToolCall>, cwd: string, serverURL: string, token: string, signal: AbortSignal): Promise<void> {
  const content = message.message.content;
  if (!Array.isArray(content)) return;
  for (const block of content as Array<Record<string, unknown>>) {
    if (block.type !== "tool_result" || typeof block.tool_use_id !== "string") continue;
    const call = toolCalls.get(block.tool_use_id);
    toolCalls.delete(block.tool_use_id);
    if (!call || block.is_error) continue;
    if (toolBaseName(call.name) !== "generate_image") continue;
    const payloads = collectGeneratedImageDescriptors(block.content);
    if (payloads.length !== 1) throw new Error(`runtime_artifact_materialization_failed: generate_image returned ${payloads.length} artifact descriptors, want exactly one`);
    const outputPath = typeof call.input.output_path === "string" ? call.input.output_path : "";
    await materializeGeneratedImage(cwd, serverURL, token, outputPath, payloads[0], signal);
  }
}

function createSeednoteStopGate(cwd: string) {
  return async (_input: unknown, _toolUseID: string | undefined, hookOptions: { signal: AbortSignal }): Promise<HookJSONOutput> => {
    try {
      const stdout = await runSeednoteGate(cwd, hookOptions.signal);
      if (!stdout.trim()) return {};
      return JSON.parse(stdout) as HookJSONOutput;
    } catch (error) {
      return { decision: "block", reason: `anban:seednote completion gate could not run: ${(error as Error).message}` };
    }
  };
}

function runSeednoteGate(cwd: string, signal: AbortSignal): Promise<string> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn("/anbanai/hooks/seednote-quality-gate.sh", [], { cwd, signal, env: { ...process.env, CLAUDE_PROJECT_DIR: cwd }, stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8").on("data", (data: string) => { if (stdout.length <= 64 << 10) stdout += data; });
    child.stderr.setEncoding("utf8").on("data", (data: string) => { if (stderr.length <= 64 << 10) stderr += data; });
    child.on("error", reject).on("close", (code) => code === 0 ? resolvePromise(stdout) : reject(new Error(stderr.trim() || `quality gate exited with ${code}`)));
    child.stdin.end(JSON.stringify({ agent_type: "anban:seednote", managed_main_session: true }));
  });
}

function requiredSkills(taskType: string): string[] {
  if (taskType === "seednote") return ["anban:agent-reach", "anban:seednote-research", "anban:seednote-viral-analysis", "anban:seednote-writing", "anban:seednote-visual-design"];
  if (taskType === "article" || taskType === "ecommerce") return ["anban:humanizer"];
  if (taskType === "live-slicer") return ["anban:live-slice", "anban:capcut-draft"];
  return [];
}

function isAllowedTool(name: string): boolean { return allowedTools.some((allowed) => allowed.endsWith("*") ? name.startsWith(allowed.slice(0, -1)) : name === allowed); }
function toolBaseName(name: string): string { return name.startsWith("mcp__") ? name.split("__", 3)[2] ?? name : name; }
