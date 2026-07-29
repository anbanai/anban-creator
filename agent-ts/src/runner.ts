import { spawn } from "node:child_process";

import { query, type HookJSONOutput, type ModelUsage, type Options, type SDKMessage, type SDKSystemMessage } from "@anthropic-ai/claude-agent-sdk";

import type { BootstrapResponse } from "./bootstrap.js";
import { collectGeneratedImageDescriptors, materializeGeneratedImage } from "./downloads.js";
import type { ExecutionResult, Reporter } from "./reporter.js";

const allowedTools = ["Read", "Write", "Edit", "Glob", "Grep", "Bash", "Skill", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TaskOutput", "TaskStop", "TodoWrite", "WebSearch", "WebFetch", "NotebookEdit", "mcp__anban__*"];
const disallowedTools = ["Agent", "ScheduleWakeup", "AskUserQuestion"];

interface TrackedToolCall { name: string; input: Record<string, unknown>; }

export async function runClaude(workspace: string, data: BootstrapResponse, serverURL: string, token: string, reporter: Reporter, signal: AbortSignal): Promise<ExecutionResult> {
  const controller = new AbortController();
  signal.addEventListener("abort", () => controller.abort(), { once: true });
  const cwd = data.task_type === "montage" ? `${workspace}/openmontage` : workspace;
  const options: Options = {
    abortController: controller,
    cwd,
    model: data.execution_profile.model_id,
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
    env: { ...process.env, ...data.execution_profile.runtime_env, ...(data.task_type === "montage" ? data.env : {}), ANBAN_API_KEY: token, ANBAN_API_URL: serverURL, ANBAN_DEFAULT_PROJECT: data.project_id },
    includePartialMessages: false,
    stderr: (line) => void reporter.progress(line.trim()),
    hooks: data.task_type === "seednote" ? { Stop: [{ hooks: [createSeednoteStopGate(cwd)] }] } : undefined,
  };
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

export function terminalModelUsage(modelUsage: Record<string, Pick<ModelUsage, "inputTokens" | "outputTokens" | "cacheReadInputTokens" | "cacheCreationInputTokens">>, aliases: BootstrapResponse["execution_profile"]["model_usage_aliases"]): { usage: Array<Record<string, string | number>>; cost_status: "reconciled" | "unreconciled"; cost_diagnostics: Array<{ code: string; raw_model?: string }> } {
  const usage: Array<Record<string, string | number>> = [];
  const diagnostics: Array<{ code: string; raw_model?: string }> = [];
  for (const raw of Object.keys(modelUsage).sort()) {
    const tokens = modelUsage[raw];
    if ([tokens.inputTokens, tokens.outputTokens, tokens.cacheReadInputTokens, tokens.cacheCreationInputTokens].some((value) => !Number.isSafeInteger(value) || value < 0)) {
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
