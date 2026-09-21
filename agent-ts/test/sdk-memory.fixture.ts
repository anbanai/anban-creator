// Executed by sdk-memory.test.ts in an isolated Bun process, never as a mock.
import assert from "node:assert/strict";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { join } from "node:path";

import { query } from "@anthropic-ai/claude-agent-sdk";
import type { ResolvedBootstrapResponse } from "../src/bootstrap.js";
import { buildQueryOptions } from "../src/runner.js";
import { prepareProjectMemory } from "../src/project-memory.js";

const root = process.argv[2]!;
const plugin = join(root, "plugin");
const agentMarker = "SDK_NATIVE_AGENT_INSTRUCTIONS_85729";
const memoryMarker = "Project editorial preference: SDK_MEMORY_RECALL_53981.";
for (const directory of [process.env.HOME!, join(plugin, ".claude-plugin"), join(plugin, "agents")]) {
  await mkdir(directory, { recursive: true });
}
await writeFile(join(plugin, ".claude-plugin/plugin.json"), JSON.stringify({ name: "anban", version: "1.0.0" }));
for (const name of ["article", "montage"]) {
  await writeFile(join(plugin, `agents/${name}.md`), `---\nname: ${name}\ndescription: Project memory regression fixture\nmodel: inherit\nmemory: project\n---\n${agentMarker}_${name}\n`);
}
process.env.CLAUDE_PLUGIN_ROOT = plugin;

let phase = "write";
let memoryFile = "";
type ModelRequest = { system: unknown; messages: unknown; model: string; stream?: boolean; tools?: { name: string }[] };
let requests: Record<string, ModelRequest[]> = { write: [], recall: [], recovery: [] };
const server = createServer(async (request, response) => {
  let raw = "";
  for await (const part of request) raw += part;
  if (request.url?.includes("count_tokens")) {
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ input_tokens: 100 }));
    return;
  }
  if (!request.url?.includes("/messages")) {
    response.writeHead(404).end("{}");
    return;
  }
  const body = JSON.parse(raw) as ModelRequest;
  // SDK session-title requests can race the main query; only the main request
  // offers Write. Do not filter by the agent marker being tested below.
  const mainRequest = body.tools?.some((tool) => tool.name === "Write") ?? false;
  if (mainRequest) requests[phase]!.push(body);
  const write = mainRequest && phase === "write" && requests.write!.length === 1;
  const block = write
    ? { type: "tool_use", id: "tool_memory_write", name: "Write", input: { file_path: memoryFile, content: `${memoryMarker}\n` } }
    : { type: "text", text: "OK" };
  const stopReason = write ? "tool_use" : "end_turn";
  const message = {
    id: `msg_memory_${phase}_${requests[phase]!.length}`, type: "message", role: "assistant", model: body.model,
    content: [block], stop_reason: stopReason, stop_sequence: null, usage: { input_tokens: 100, output_tokens: 10 },
  };
  if (!body.stream) {
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify(message));
    return;
  }
  response.setHeader("content-type", "text/event-stream");
  const events = [
    ["message_start", { type: "message_start", message: { ...message, content: [], stop_reason: null, usage: { input_tokens: 100, output_tokens: 0 } } }],
    ["content_block_start", { type: "content_block_start", index: 0, content_block: write ? { ...block, input: {} } : { type: "text", text: "" } }],
    ["content_block_delta", { type: "content_block_delta", index: 0, delta: write ? { type: "input_json_delta", partial_json: JSON.stringify(block.input) } : { type: "text_delta", text: "OK" } }],
    ["content_block_stop", { type: "content_block_stop", index: 0 }],
    ["message_delta", { type: "message_delta", delta: { stop_reason: stopReason, stop_sequence: null }, usage: { output_tokens: 10 } }],
    ["message_stop", { type: "message_stop" }],
  ];
  for (const [event, data] of events) response.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
  response.end();
});
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const address = server.address();
assert(address && typeof address === "object");
const baseURL = `http://127.0.0.1:${address.port}`;
const controller = new AbortController();
const deadline = setTimeout(() => controller.abort(), 45_000);
try {
  for (const taskType of ["article", "montage"]) {
    const adapter = taskType === "montage" ? "openmontage" : "standard";
    let previousMemoryRoot: string | undefined;
    requests = { write: [], recall: [], recovery: [] };
    for (const currentPhase of ["write", "recall", "recovery"]) {
      phase = currentPhase;
      const workspace = join(root, `workspace-${taskType}-${phase}`);
      const cwd = adapter === "openmontage" ? join(workspace, "openmontage") : workspace;
      await mkdir(join(cwd, ".claude"), { recursive: true });
      const sharedMemory = join(cwd, ".claude/agent-memory");
      // Moving the same real directory simulates the project mount in the next
      // ephemeral workspace without requiring root privileges or bind mounts.
      if (previousMemoryRoot) await rename(previousMemoryRoot, sharedMemory);
      else await mkdir(sharedMemory);
      previousMemoryRoot = sharedMemory;
      const data = {
        execution_token: "fixture-execution-token", execution_id: "fixture-execution", task_type: taskType, project_id: "fixture-project",
        agent_pack_id: taskType, agent_pack_version: "1.0.0", agent_pack_digest: "a".repeat(64),
        runtime_profile: taskType, runtime_adapter: adapter, agent_flag: `anban:${taskType}`, max_turns: 3,
        prompt: "Exercise project memory.", artifact_transport: { mode: "stream" }, agent_memory_directory: ".claude/agent-memory",
        resolved_agent_pack: {
          id: taskType, version: "1.0.0", digest: "a".repeat(64), agent: { name: taskType },
          bindings: { task_types: [taskType] }, runtime: { profile: taskType, adapter }, artifacts: [],
        },
        execution_profile: {
          profile_id: "fixture", provider: "anthropic", protocol: "anthropic", display_name: "Local fixture", profile_fingerprint: "a".repeat(64),
          envs: { ANTHROPIC_BASE_URL: baseURL, ANTHROPIC_API_KEY: "local-fixture-only", ANTHROPIC_MODEL: "claude-sonnet-4-6", MAX_THINKING_TOKENS: "0", ENABLE_TOOL_SEARCH: "false" },
          model_usage_aliases: {},
        },
      } as ResolvedBootstrapResponse;
      if (phase === "recovery") data.agent_memory_directory = undefined;
      await prepareProjectMemory(workspace, data);
      const options = buildQueryOptions(data, workspace, baseURL, "fixture-token", undefined, controller);
      memoryFile = join(options.cwd!, `.claude/agent-memory/anban-${taskType}/MEMORY.md`);
      // The regression covers the real production agent, prompt, settings, tools and
      // environment. Remote MCP and business completion hooks are unrelated here.
      options.mcpServers = {};
      options.hooks = {};
      let result: string | undefined;
      for await (const message of query({ prompt: phase === "write" ? "Save the project preference in memory." : "Recall the project preference.", options })) {
        if (message.type === "result") result = message.subtype;
      }
      assert.equal(result, "success", `${phase} SDK query must complete successfully`);
      assert(requests[phase]!.length > 0, `${phase} must reach the local Anthropic server`);
      const firstPrompt = JSON.stringify({ system: requests[phase]![0].system, messages: requests[phase]![0].messages });
      assert(firstPrompt.includes(`${agentMarker}_${taskType}`), "actual model request must contain selected plugin agent instructions");
      if (phase !== "recovery") {
        assert(firstPrompt.includes("# Persistent Agent Memory"), "actual model request must contain native agent memory instructions");
        assert(firstPrompt.includes(join(options.cwd!, `.claude/agent-memory/anban-${taskType}`)), "actual model request must use the native agent memory directory");
      } else {
        assert(!firstPrompt.includes(memoryMarker), "clean policy recovery must not load prior project memory");
      }
      if (phase === "write") {
        const messages = requests.write!.at(-1)!.messages as { role: string; content: unknown }[];
        const toolResult = JSON.stringify(messages.at(-1));
        assert(!toolResult.includes('"is_error":true'), `real SDK ${taskType} Write failed: ${toolResult}`);
      }
      assert.equal(await readFile(join(sharedMemory, `anban-${taskType}/MEMORY.md`), "utf8"), `${memoryMarker}\n`, "real SDK Write must persist project memory");
      if (phase === "recall") assert(firstPrompt.includes(memoryMarker), "a fresh session in a fresh workspace must load prior project memory");
    }
  }
  console.log("SDK project memory write and fresh-session recall passed");
} finally {
  clearTimeout(deadline);
  server.closeAllConnections();
  await new Promise<void>((resolve) => server.close(() => resolve()));
}
