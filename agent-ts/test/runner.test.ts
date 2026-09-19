import { describe, expect, mock, test } from "bun:test";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { HookCallback } from "@anthropic-ai/claude-agent-sdk";

import type { AgentPack } from "../src/bootstrap.js";
import type { StageProgressEvent } from "../src/reporter.js";
import * as runner from "../src/runner.js";

const { buildExecutionEnvironment, terminalModelUsage, validateManagedInit } = runner;

const articlePack: AgentPack = {
  id: "article",
  version: "1.0.0",
  digest: "a".repeat(64),
  agent: { name: "article" },
  bindings: { task_types: ["article"] },
  runtime: { profile: "article", adapter: "standard" },
  artifacts: [{ role: "analysis", path: "output/topic-analysis.md", required: true }],
};

const viralAnalysisPack: AgentPack = {
  ...articlePack,
  id: "seednote",
  agent: { name: "seednote" },
  bindings: { task_types: ["seednote", "viral_analysis"] },
  runtime: { profile: "seednote", adapter: "standard" },
  artifacts: [
    { role: "analysis", path: "output/source-analysis.md", required: true },
    { role: "template", path: "output/viral-template.json", required: true },
  ],
};

const finalArtifactPack: AgentPack = {
  ...articlePack,
  artifacts: [{ role: "final", path: "output/final.md", required: true }],
};

const validBootstrap = () => ({
  execution_token: "execution-token",
  execution_id: "execution-1",
  task_type: "article",
  agent_pack_id: "article",
  agent_pack_version: "1.0.0",
  agent_pack_digest: "a".repeat(64),
  runtime_profile: "article",
  runtime_adapter: "standard",
  project_id: "project-1",
  max_turns: 10,
  agent_flag: "anban:article",
  prompt: "write",
  artifact_transport: { mode: "stream" },
  resolved_agent_pack: articlePack,
  execution_profile: {
    profile_id: "quality",
    provider: "moonshot",
    protocol: "anthropic",
    display_name: "极致效果",
    profile_fingerprint: "a".repeat(64),
    envs: {
      ANTHROPIC_BASE_URL: "https://api.moonshot.cn/anthropic",
      ANTHROPIC_AUTH_TOKEN: "secret",
      ANTHROPIC_MODEL: "kimi-k3[1m]",
      ANTHROPIC_DEFAULT_OPUS_MODEL: "kimi-k3[1m]",
      ANTHROPIC_DEFAULT_FABLE_MODEL: "kimi-k3[1m]",
      ANTHROPIC_DEFAULT_SONNET_MODEL: "kimi-k3[1m]",
      ANTHROPIC_DEFAULT_HAIKU_MODEL: "kimi-k3[1m]",
      MAX_THINKING_TOKENS: "0",
      ENABLE_TOOL_SEARCH: "false",
    },
    model_usage_aliases: { "kimi-k3[1m]": { provider: "moonshot", model: "kimi-k3" } },
  },
});

const hookOptions = { signal: new AbortController().signal };

function taskHookInput(toolName: "TaskCreate" | "TaskUpdate", toolInput: Record<string, unknown>, toolResponse: unknown = {}): Parameters<HookCallback>[0] {
  return {
    session_id: "session-1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PostToolUse",
    tool_name: toolName,
    tool_input: toolInput,
    tool_response: toolResponse,
    tool_use_id: "tool-1",
  };
}

function stopHookInput(stopHookActive = false, backgroundTasks: unknown[] = []): Parameters<HookCallback>[0] {
  return {
    session_id: "session-1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "Stop",
    stop_hook_active: stopHookActive,
    background_tasks: backgroundTasks,
  };
}

function optionHooks(options: ReturnType<typeof runner.buildQueryOptions>) {
  return options.hooks!;
}

async function progressWorkspace(withResearchArtifact = false): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "anban-runner-hooks-"));
  await mkdir(join(root, "output"));
  if (withResearchArtifact) await writeFile(join(root, "output", "topic-analysis.md"), "research");
  return root;
}

describe("validateManagedInit", () => {
  test("requires the configured remote MCP and article plugin skill", () => {
    expect(() => validateManagedInit({ type: "system", subtype: "init", mcp_servers: [{ name: "anban", status: "connected" }], plugins: [{ name: "anban", path: "/anbanai" }], tools: [], skills: [] }, "article")).toThrow("anban:humanizer");
  });

  test("requires Seednote core tools and all publishing skills while research tools remain optional", () => {
    const message = {
      type: "system" as const,
      subtype: "init" as const,
      mcp_servers: [{ name: "anban", status: "connected" as const }],
      plugins: [{ name: "anban", path: "/anbanai" }],
      tools: [
        "mcp__anban__analyze_image",
        "mcp__anban__claim_topic",
        "mcp__anban__finalize_task_title",
        "mcp__anban__generate_image",
        "mcp__anban__get_project_profile",
        "mcp__anban__get_seednote_feed_detail",
        "mcp__anban__get_seednote_user_profile",
        "mcp__anban__list_project_titles",
        "mcp__anban__search_seednote_feeds",
        "mcp__anban__submit_agent_feedback",
      ],
      skills: [
        "anban:seednote-research",
        "anban:seednote-viral-analysis",
        "anban:seednote-writing",
        "anban:seednote-visual-design",
        "anban:humanizer",
      ],
    };
    expect(() => validateManagedInit(message, "seednote")).not.toThrow();
    message.tools = message.tools.filter((tool) => tool !== "mcp__anban__search_seednote_feeds");
    expect(() => validateManagedInit(message, "seednote")).not.toThrow();
    message.tools = message.tools.filter((tool) => tool !== "mcp__anban__generate_image");
    expect(() => validateManagedInit(message, "seednote")).toThrow("generate_image");
    message.tools = [...message.tools, "mcp__anban__generate_image"];
    message.skills = message.skills.slice(1);
    expect(() => validateManagedInit(message, "seednote")).toThrow("anban:seednote-research");
    message.skills = [
      "anban:seednote-research",
      "anban:seednote-viral-analysis",
      "anban:seednote-writing",
      "anban:seednote-visual-design",
    ];
    expect(() => validateManagedInit(message, "seednote")).toThrow("anban:humanizer");
  });

  test("requires only research skills for viral analysis", () => {
    const message = {
      type: "system" as const,
      subtype: "init" as const,
      mcp_servers: [{ name: "anban", status: "connected" as const }],
      plugins: [{ name: "anban", path: "/anbanai" }],
      tools: [
        "mcp__anban__get_project_profile",
        "mcp__anban__list_project_titles",
        "mcp__anban__submit_agent_feedback",
      ],
      skills: [
        "anban:seednote-research",
        "anban:seednote-viral-analysis",
      ],
    };
    expect(() => validateManagedInit(message, "viral_analysis")).not.toThrow();
    message.tools = message.tools.filter((tool) => tool !== "mcp__anban__get_project_profile");
    expect(() => validateManagedInit(message, "viral_analysis")).toThrow("get_project_profile");
    message.tools = [...message.tools, "mcp__anban__get_project_profile"];
    message.skills = [];
    expect(() => validateManagedInit(message, "viral_analysis")).toThrow("anban:seednote-research");
  });

  test("does not implicitly require legacy progress for other managed task types", () => {
    const taskTypes = [
      { taskType: "article", tool: "get_project_profile", skills: ["anban:humanizer"] },
      { taskType: "ecommerce", tool: "get_project_profile", skills: ["anban:humanizer"] },
      { taskType: "live-slicer", tool: "analyze_video", skills: ["anban:live-slice", "anban:capcut-draft"] },
      { taskType: "moments", tool: "get_project_profile", skills: [] },
    ];
    for (const fixture of taskTypes) {
      expect(() => validateManagedInit({
        type: "system",
        subtype: "init",
        mcp_servers: [{ name: "anban", status: "connected" }],
        plugins: [{ name: "anban", path: "/anbanai" }],
        tools: [`mcp__anban__${fixture.tool}`],
        skills: fixture.skills,
      }, fixture.taskType)).not.toThrow();
    }
  });

  test("requires the complete Montage cover runtime surface", () => {
    const requiredTools = [
      "analyze_image",
      "analyze_video",
      "generate_image",
      "get_project_profile",
      "submit_agent_feedback",
    ];
    const requiredSkills = ["anban:montage", "anban:video-cover-design"];
    const message = {
      type: "system" as const,
      subtype: "init" as const,
      mcp_servers: [{ name: "anban", status: "connected" as const }],
      plugins: [{ name: "anban", path: "/anbanai" }],
      tools: requiredTools.map((tool) => `mcp__anban__${tool}`),
      skills: requiredSkills,
    };

    expect(() => validateManagedInit(message, "montage")).not.toThrow();
    for (const missing of requiredTools) {
      expect(() => validateManagedInit({
        ...message,
        tools: message.tools.filter((tool) => tool !== `mcp__anban__${missing}`),
      }, "montage"), missing).toThrow(missing);
    }
    for (const missing of requiredSkills) {
      expect(() => validateManagedInit({
        ...message,
        skills: message.skills.filter((skill) => skill !== missing),
      }, "montage"), missing).toThrow(missing);
    }
  });

  test("maps terminal SDK usage through the bootstrap model aliases", () => {
    expect(terminalModelUsage({ raw: { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 } }, { raw: { provider: "anthropic", model: "claude-sonnet" } })).toEqual({ usage: [{ provider: "anthropic", model: "claude-sonnet", input_tokens: 3, output_tokens: 5, cache_read_input_tokens: 7, cache_creation_input_tokens: 11 }], cost_status: "reconciled", cost_diagnostics: [] });
  });

  test("keeps all mapped terminal model rows", () => {
    const got = terminalModelUsage({
      "kimi-k3[1m]": { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 },
      "kimi-k2.7-code": { inputTokens: 13, outputTokens: 17, cacheReadInputTokens: 19, cacheCreationInputTokens: 23 },
    }, {
      "kimi-k3[1m]": { provider: "moonshot", model: "kimi-k3" },
      "kimi-k2.7-code": { provider: "moonshot", model: "kimi-k2.7-code" },
    });
    expect(got.cost_status).toBe("reconciled");
    expect(got.usage).toHaveLength(2);
  });

  test("keeps mapped rows but marks mixed mapped and unmapped usage unreconciled", () => {
    const got = terminalModelUsage({
      mapped: { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 },
      unknown: { inputTokens: 13, outputTokens: 17, cacheReadInputTokens: 19, cacheCreationInputTokens: 23 },
    }, { mapped: { provider: "moonshot", model: "kimi-k3" } });
    expect(got.cost_status).toBe("unreconciled");
    expect(got.usage).toEqual([{ provider: "moonshot", model: "kimi-k3", input_tokens: 3, output_tokens: 5, cache_read_input_tokens: 7, cache_creation_input_tokens: 11 }]);
    expect(got.cost_diagnostics).toEqual([{ code: "unmapped_model_usage_alias", raw_model: "unknown" }]);
  });

  test("keeps mapped rows when another terminal usage row is malformed", () => {
    const got = terminalModelUsage({
      mapped: { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 },
      broken: null,
    } as unknown as Parameters<typeof terminalModelUsage>[0], {
      mapped: { provider: "moonshot", model: "kimi-k3" },
      broken: { provider: "moonshot", model: "kimi-k2.7-code" },
    });
    expect(got.cost_status).toBe("unreconciled");
    expect(got.usage).toEqual([{ provider: "moonshot", model: "kimi-k3", input_tokens: 3, output_tokens: 5, cache_read_input_tokens: 7, cache_creation_input_tokens: 11 }]);
    expect(got.cost_diagnostics).toEqual([{ code: "invalid_model_usage_tokens", raw_model: "broken" }]);
  });
});

describe("buildQueryOptions", () => {
  test("enables the bounded read-only worker policy only for Article and Seednote task types", () => {
    const article = runner.buildQueryOptions(validBootstrap(), "/workspace");
    const seednote = runner.buildQueryOptions({
      ...validBootstrap(), task_type: "seednote", agent_pack_id: "seednote", runtime_profile: "seednote", agent_flag: "anban:seednote", resolved_agent_pack: viralAnalysisPack,
    }, "/workspace");
    const viral = runner.buildQueryOptions({
      ...validBootstrap(), task_type: "viral_analysis", agent_pack_id: "seednote", runtime_profile: "seednote", agent_flag: "anban:seednote", resolved_agent_pack: viralAnalysisPack,
    }, "/workspace");

    for (const options of [article, seednote, viral]) {
      expect(options.agent).toMatch(/^anban:(article|seednote)$/);
      expect(options.allowedTools).toContain("Agent");
      expect(options.disallowedTools).not.toContain("Agent");
      const worker = options.agents?.["managed-readonly-worker"];
      expect(worker).toMatchObject({ model: "inherit", maxTurns: 12, background: false });
    }

    const articleWorker = article.agents?.["managed-readonly-worker"];
    expect(articleWorker?.tools).toEqual(["Read", "Glob", "Grep", "WebSearch", "WebFetch", "mcp__anban__get_project_profile", "mcp__anban__list_project_titles"]);
    expect(articleWorker?.skills).toEqual(["topic-research", "seo-optimization"]);
    for (const options of [seednote, viral]) {
      const worker = options.agents?.["managed-readonly-worker"];
      expect(worker?.tools).toEqual([
        "Read", "Glob", "Grep", "WebSearch", "WebFetch",
        "mcp__anban__get_project_profile", "mcp__anban__list_project_titles",
        "mcp__anban__search_seednote_feeds", "mcp__anban__get_seednote_feed_detail", "mcp__anban__get_seednote_user_profile",
      ]);
      expect(worker?.skills).toEqual(["seednote-research", "seednote-viral-analysis"]);
    }

    const ineligible = runner.buildQueryOptions({ ...validBootstrap(), task_type: "montage" }, "/workspace");
    expect(ineligible.agents).toBeUndefined();
    expect(ineligible.allowedTools).not.toContain("Agent");
    expect(ineligible.disallowedTools).toContain("Agent");
  });

  test("adds the managed foreground worker contract only to eligible prompts", () => {
    expect(runner.buildManagedPrompt(validBootstrap())).toContain("managed-readonly-worker");
    expect(runner.buildManagedPrompt(validBootstrap())).toContain("run_in_background: false");
    expect(runner.buildManagedPrompt(validBootstrap())).toContain("at least two independent research, material-analysis, or quality-review tasks");
    expect(runner.buildManagedPrompt(validBootstrap())).toContain("at most three concurrent Workers");
    expect(runner.buildManagedPrompt({ ...validBootstrap(), task_type: "montage" })).not.toContain("managed-readonly-worker");
  });

  test("rewrites allowed worker invocations to foreground and denies other Agent types", async () => {
    const eligible = runner.buildQueryOptions(validBootstrap(), "/workspace");
    const ineligible = runner.buildQueryOptions({ ...validBootstrap(), task_type: "montage" }, "/workspace");
    const eligibleAgentHook = eligible.hooks!.PreToolUse!.find((entry) => entry.matcher === "Agent")!.hooks[0]!;
    const ineligibleAgentHook = ineligible.hooks!.PreToolUse!.find((entry) => entry.matcher === "Agent")!.hooks[0]!;
    const agentInput = (subagent_type: string): Parameters<HookCallback>[0] => ({
      session_id: "session-1",
      transcript_path: "/tmp/transcript.jsonl",
      cwd: "/workspace",
      hook_event_name: "PreToolUse",
      tool_name: "Agent",
      tool_input: {
        description: "Research source material",
        prompt: "Find relevant sources.",
        subagent_type,
        model: "opus",
        isolation: "worktree",
        run_in_background: true,
      },
      tool_use_id: "agent-tool-1",
    });

    await expect(eligibleAgentHook(agentInput("managed-readonly-worker"), "agent-tool-1", hookOptions)).resolves.toEqual({
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        updatedInput: {
          description: "Research source material",
          prompt: "Find relevant sources.",
          subagent_type: "managed-readonly-worker",
          run_in_background: false,
        },
      },
    });
    await expect(eligibleAgentHook(agentInput("untrusted-worker"), "agent-tool-1", hookOptions)).resolves.toMatchObject({
      hookSpecificOutput: { permissionDecision: "deny" },
    });
    await expect(ineligibleAgentHook(agentInput("managed-readonly-worker"), "agent-tool-1", hookOptions)).resolves.toMatchObject({
      hookSpecificOutput: { permissionDecision: "deny" },
    });
  });

  test("forces managed worker tasks to disable background work after frozen environment values", () => {
    const options = runner.buildQueryOptions({
      ...validBootstrap(),
      execution_profile: { ...validBootstrap().execution_profile, envs: { ...validBootstrap().execution_profile.envs, CLAUDE_CODE_DISABLE_BACKGROUND_TASKS: "0" } },
    }, "/workspace");
    expect(options.env?.CLAUDE_CODE_DISABLE_BACKGROUND_TASKS).toBe("1");
    expect(runner.buildQueryOptions({ ...validBootstrap(), task_type: "montage" }, "/workspace").env?.CLAUDE_CODE_DISABLE_BACKGROUND_TASKS).toBeUndefined();
  });

  test("blocks main Stop while managed background tasks remain before completion processing", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    try {
      const stop = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, {
        progress: async () => {}, stageProgress,
      })).Stop![0]!.hooks[0]!;

      for (const stopHookActive of [false, true]) {
        await expect(stop(stopHookInput(stopHookActive, [{ task_id: "worker-1" }]), undefined, hookOptions)).resolves.toMatchObject({
          decision: "block",
          hookSpecificOutput: { hookEventName: "Stop", additionalContext: expect.stringContaining("pending") },
        });
      }
      expect(stageProgress).not.toHaveBeenCalled();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("keeps completion on main Stop and allows normal Stop continuation", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    try {
      const hooks = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, {
        progress: async () => {}, stageProgress,
      }));
      expect(hooks.SubagentStop).toBeUndefined();
      await expect(hooks.Stop![0]!.hooks[0]!(stopHookInput(), undefined, hookOptions)).resolves.toEqual({});
      expect(stageProgress).not.toHaveBeenCalled();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("builds the managed prompt with resume context", () => {
    const data = {
      ...validBootstrap(),
      resume_session_id: "session-1",
      resume_context_path: ".anban-creator/resume/executions/execution-1/latest.md",
    };
    expect(runner.buildManagedPrompt(data)).toContain(data.resume_context_path);
    expect(runner.buildQueryOptions(data, "/workspace").resume).toBe("session-1");
  });

  test("uses the frozen runtime adapter for the working directory", () => {
    const data = { ...validBootstrap(), task_type: "montage", runtime_adapter: "standard" };
    expect(runner.buildQueryOptions(data, "/workspace").cwd).toBe("/workspace");
    expect(runner.buildQueryOptions({ ...data, runtime_adapter: "openmontage" }, "/workspace").cwd).toBe("/workspace/openmontage");
  });

  test("loads project settings, auto memory, and the writable Montage root", () => {
    const data = { ...validBootstrap(), task_type: "montage", runtime_adapter: "openmontage", auto_memory_directory: ".claude/memory" };
    const options = runner.buildQueryOptions(data, "/tasks/task-1");
    expect(options.settingSources).toEqual(["user", "project"]);
    expect(options.settings).toEqual({ autoMemoryEnabled: true, autoMemoryDirectory: "/tasks/task-1/.claude/memory" });
    expect(options.env?.ANBAN_MONTAGE_SUBMODULE_PATH).toBe("/tasks/task-1/openmontage");
  });

  test("does not enable project auto memory when the bootstrap omits its directory", () => {
    expect(runner.buildQueryOptions(validBootstrap(), "/workspace").settings).toBeUndefined();
  });

  test("keeps the managed tool allowlist fail-closed", async () => {
    const options = runner.buildQueryOptions(validBootstrap(), "/workspace");

    expect(options.permissionMode).toBe("default");
    expect(options.allowDangerouslySkipPermissions).toBeUndefined();
    expect(options.allowedTools).toEqual(expect.arrayContaining(["Read", "Write", "Bash", "WebFetch", "mcp__anban__*"]));
    expect(options.disallowedTools).toEqual(["ScheduleWakeup", "AskUserQuestion"]);
    expect(await options.canUseTool?.("UnknownTool", {}, { signal: new AbortController().signal, toolUseID: "tool-1", requestId: "request-1" })).toEqual({
      behavior: "deny",
      message: 'tool "UnknownTool" is outside the managed Agent SDK allowlist',
    });
  });

  test("enforces the MCP boundary and composes the Seednote stop gate for both Seednote task types", async () => {
    const articleOptions = runner.buildQueryOptions(validBootstrap(), "/workspace");
    const articleHooks = articleOptions.hooks as Record<string, Array<{ hooks: Array<(input: unknown, toolUseID: string | undefined, options: { signal: AbortSignal }) => Promise<Record<string, unknown>>> }>>;
    expect(articleHooks.PreToolUse).toHaveLength(2);
    expect(articleHooks.PostToolUse).toHaveLength(1);
    expect(articleHooks.PostToolUse[0]).toMatchObject({ matcher: "TaskCreate|TaskUpdate" });
    expect(articleHooks.Stop).toHaveLength(1);
    expect(articleHooks.Stop[0].hooks).toHaveLength(1);

    const boundary = articleHooks.PreToolUse[0].hooks[0];
    const hookOptions = { signal: new AbortController().signal };
    const direct = await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "curl http://sidecar-seednote:18060/mcp" }, tool_use_id: "tool-1" }, "tool-1", hookOptions);
    expect(direct).toMatchObject({ hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny" } });
    const directRest = await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "curl http://sidecar-seednote:18060/api/v1/feeds" }, tool_use_id: "tool-rest" }, "tool-rest", hookOptions);
    expect(directRest).toMatchObject({ hookSpecificOutput: { permissionDecision: "deny" } });
    expect(await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "printf done" }, tool_use_id: "tool-2" }, "tool-2", hookOptions)).toEqual({});

    for (const taskType of ["seednote", "viral_analysis"]) {
      const options = runner.buildQueryOptions({ ...validBootstrap(), task_type: taskType }, "/workspace");
      const hooks = options.hooks as Record<string, unknown[]>;
      expect(hooks.PreToolUse).toHaveLength(2);
      expect(hooks.Stop).toHaveLength(1);
      expect((hooks.Stop[0] as { hooks: unknown[] }).hooks).toHaveLength(1);
    }
  });

  test("preserves the concrete task type in Seednote quality gate input", () => {
    expect(runner.seednoteGateInput("seednote")).toEqual({ agent_type: "anban:seednote", managed_main_session: true, task_type: "seednote" });
    expect(runner.seednoteGateInput("viral_analysis")).toEqual({ agent_type: "anban:seednote", managed_main_session: true, task_type: "viral_analysis" });
  });

  test("does not translate frozen controls into SDK-only options", () => {
    const options = (runner as typeof runner & {
      buildQueryOptions: (data: unknown, workspace: string) => Record<string, unknown>;
    }).buildQueryOptions(validBootstrap(), "/workspace");
    expect(options).not.toHaveProperty("effort");
    expect(options).not.toHaveProperty("thinking");
    expect(options).not.toHaveProperty("model");
    expect(options.env).toMatchObject({
      ANTHROPIC_MODEL: "kimi-k3[1m]",
      MAX_THINKING_TOKENS: "0",
      ENABLE_TOOL_SEARCH: "false",
    });
  });
});

describe("managed progress hooks", () => {
  test("translates Task metadata into lifecycle updates without Pack stage assumptions", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    try {
      const callback = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, {
        progress: async () => {},
        stageProgress,
      })).PostToolUse![0]!.hooks[0]!;

      expect(await callback(taskHookInput("TaskCreate", {
        subject: "Review sources",
        description: "Review the supplied sources",
        metadata: { anban_stage_id: "source_review" },
      }, { task: { id: "stage-task" } }), "tool-create", hookOptions)).toEqual({});

      expect(await callback(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "in_progress",
        description: "正在核对来源",
        metadata: { anban_stage_id: "source_review" },
      }, { success: true }), "tool-active", hookOptions)).toEqual({});
      expect(await callback(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "completed",
        description: "来源核对完成",
        metadata: { anban_stage_id: "source_review" },
      }, { success: true }), "tool-complete", hookOptions)).toEqual({});

      expect(stageProgress).toHaveBeenNthCalledWith(1, {
        stage: "source_review",
        state: "active",
        description: "正在核对来源",
      }, hookOptions.signal);
      expect(stageProgress).toHaveBeenNthCalledWith(2, {
        stage: "source_review",
        state: "complete",
        description: "来源核对完成",
      }, hookOptions.signal);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("ignores non-lifecycle tasks and keeps annotated TaskCreate mapping stable", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const diagnostics: string[] = [];
    try {
      const emitter = new (await import("../src/progress.js")).ProgressEmitter({ stageProgress }, (message) => diagnostics.push(message));
      const callback = runner.createTaskProgressHook(emitter, (message) => diagnostics.push(message));

      expect(await callback(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "in_progress",
      }), "tool-missing", hookOptions)).toEqual({});
      expect(stageProgress).not.toHaveBeenCalled();
      expect(diagnostics).toEqual([]);

      await callback(taskHookInput("TaskCreate", {
        metadata: { anban_stage_id: "source_review" },
      }, { task: { id: "stage-task" } }), "tool-create", hookOptions);
      await callback(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "in_progress",
        metadata: { anban_stage_id: "writing" },
      }, { success: true }), "tool-conflict", hookOptions);

      expect(stageProgress).not.toHaveBeenCalled();
      expect(diagnostics.join("\n")).toContain("does not match TaskCreate stage source_review");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("ignores an explicitly failed TaskUpdate response", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    try {
      const callback = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, {
        progress: async () => {},
        stageProgress,
      })).PostToolUse![0]!.hooks[0]!;

      await callback(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "in_progress",
        metadata: { anban_stage_id: "source_review" },
      }, { success: false, error: "not found" }), "tool-failed", hookOptions);

      expect(stageProgress).not.toHaveBeenCalled();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("validates required Pack artifacts only at Stop", async () => {
    const root = await progressWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    try {
      const hooks = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, {
        progress: async () => {},
        stageProgress,
      }));
      const taskProgress = hooks.PostToolUse![0]!.hooks[0]!;
      const stop = hooks.Stop![0]!.hooks[0]!;

      expect(await taskProgress(taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "completed",
        metadata: { anban_stage_id: "delivery" },
      }, { success: true }), "tool-complete", hookOptions)).toEqual({});
      expect(stageProgress).toHaveBeenCalledWith({
        stage: "delivery",
        state: "complete",
        description: undefined,
      }, hookOptions.signal);

      expect(await stop(stopHookInput(), undefined, hookOptions)).toMatchObject({
        decision: "block",
        hookSpecificOutput: { additionalContext: expect.stringContaining("output/topic-analysis.md") },
      });

      await writeFile(join(root, "output", "topic-analysis.md"), "ready");
      expect(await stop(stopHookInput(), undefined, hookOptions)).toEqual({});
      expect(stageProgress).toHaveBeenCalledTimes(1);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("uses the resolved task-type artifact override at Stop", async () => {
    const root = await progressWorkspace();
    const pluginRoot = join(root, "plugin");
    await mkdir(join(pluginRoot, "hooks"), { recursive: true });
    await writeFile(join(pluginRoot, "hooks", "seednote-quality-gate.sh"), "#!/bin/sh\nexit 0\n");
    await chmod(join(pluginRoot, "hooks", "seednote-quality-gate.sh"), 0o755);
    await writeFile(join(root, "output", "source-analysis.md"), "analysis");
    const previousPluginRoot = process.env.CLAUDE_PLUGIN_ROOT;
    process.env.CLAUDE_PLUGIN_ROOT = pluginRoot;
    try {
      const stop = optionHooks(runner.buildQueryOptions({
        ...validBootstrap(),
        task_type: "viral_analysis",
        agent_pack_id: "seednote",
        runtime_profile: "seednote",
        agent_flag: "anban:seednote",
        resolved_agent_pack: viralAnalysisPack,
      }, root)).Stop![0]!.hooks[0]!;

      expect(await stop(stopHookInput(), undefined, hookOptions)).toMatchObject({
        decision: "block",
        hookSpecificOutput: { additionalContext: expect.stringContaining("output/viral-template.json") },
      });
      await writeFile(join(root, "output", "viral-template.json"), "{}");
      expect(await stop(stopHookInput(), undefined, hookOptions)).toEqual({});
    } finally {
      if (previousPluginRoot === undefined) delete process.env.CLAUDE_PLUGIN_ROOT;
      else process.env.CLAUDE_PLUGIN_ROOT = previousPluginRoot;
      await rm(root, { recursive: true, force: true });
    }
  });

  test("creates isolated lifecycle event state for each query options instance", async () => {
    const root = await progressWorkspace(true);
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const reporter = { progress: async () => {}, stageProgress };
    try {
      const first = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, reporter)).PostToolUse![0]!.hooks[0]!;
      const second = optionHooks(runner.buildQueryOptions(validBootstrap(), root, undefined, undefined, reporter)).PostToolUse![0]!.hooks[0]!;
      const input = taskHookInput("TaskUpdate", {
        taskId: "stage-task",
        status: "in_progress",
        metadata: { anban_stage_id: "source_review" },
      }, { success: true });

      await first(input, "tool-1", hookOptions);
      await first(input, "tool-1", hookOptions);
      await second(input, "tool-1", hookOptions);
      expect(stageProgress).toHaveBeenCalledTimes(2);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});

describe("recordAssistantToolUses", () => {
  test("counts assistant tool uses for terminal diagnostics", () => {
    const diagnostics = { tool_use_count: 0, tool_use_summary: {} as Record<string, number> };

    runner.recordAssistantToolUses([
      { type: "tool_use", id: "tool-1", name: "Agent", input: {} },
      { type: "text", text: "delegating" },
      { type: "tool_use", id: "tool-2", name: "mcp__anban__write_article", input: {} },
      { type: "tool_use", id: "tool-3", name: "Agent", input: {} },
    ], diagnostics);

    expect(diagnostics).toEqual({
      tool_use_count: 3,
      tool_use_summary: { Agent: 2, mcp__anban__write_article: 1 },
    });
  });

  test("includes tool-use diagnostics in the terminal execution result", () => {
    const terminal = runner.terminalExecutionResult({
      type: "result",
      subtype: "success",
      session_id: "session-1",
      num_turns: 2,
      duration_ms: 100,
      duration_api_ms: 80,
      modelUsage: {},
    } as Parameters<typeof runner.terminalExecutionResult>[0], "/workspace", "done", {}, {
      tool_use_count: 2,
      tool_use_summary: { Agent: 2 },
    });

    expect(terminal).toMatchObject({
      success: true,
      tool_use_count: 2,
      tool_use_summary: { Agent: 2 },
    });
  });

  test("treats an SDK success subtype with an API policy error as failure", () => {
    const terminal = runner.terminalExecutionResult({
      type: "result",
      subtype: "success",
      is_error: true,
      terminal_reason: "api_error",
      api_error_status: 400,
      result: "API Error: 400 Content Exists Risk (request_id: req_01ABCxyz)",
      session_id: "session-1",
      num_turns: 8,
      duration_ms: 100,
      duration_api_ms: 80,
      modelUsage: {},
    } as Parameters<typeof runner.terminalExecutionResult>[0], "/workspace", "done", {}, {
      tool_use_count: 2,
      tool_use_summary: { Write: 2 },
    });

    expect(terminal).toMatchObject({
      success: false,
      error: "供应商内容安全策略拒绝了本次请求。",
      terminal_reason: "provider_error",
      error_code: "provider_policy_rejection",
      policy_domain: "content_safety",
      provider_code: "Content Exists Risk",
      http_status: 400,
      content_direction: "unknown",
      recoverable: true,
      request_id: "req_01ABCxyz",
      failure_stage: "provider_request",
      resume_from: "provider_request",
    });
  });
});

describe("recordTrustedToolError", () => {
  test("records a platform identity failure only from an exact fixed-SKU MCP tool result", () => {
    const diagnostics = { tool_use_count: 0, tool_use_summary: {} as Record<string, number> };

    runner.recordTrustedToolError("mcp__anban__generate_image", [{
      type: "text",
      text: JSON.stringify({ code: "execution_identity_required", message: "credential unavailable" }),
    }], diagnostics);

    expect(diagnostics).toMatchObject({
      root_error_code: "execution_identity_unavailable",
      failure_stage: "image_generation",
      resume_from: "image_generation",
    });
  });

  test("ignores identity-shaped text from other tools and mismatch errors", () => {
    for (const [tool, code] of [
      ["mcp__anban__write_article", "execution_identity_required"],
      ["mcp__anban__generate_image", "execution_identity_mismatch"],
    ] as const) {
      const diagnostics = { tool_use_count: 0, tool_use_summary: {} as Record<string, number> };
      runner.recordTrustedToolError(tool, [{ type: "text", text: JSON.stringify({ code }) }], diagnostics);
      expect(diagnostics).not.toHaveProperty("root_error_code");
    }
  });
});

describe("buildExecutionEnvironment", () => {
  test("applies frozen runtime environment after execution identity", () => {
    const managed = buildExecutionEnvironment(
      {
        ANTHROPIC_MODEL: "process-model",
        ANTHROPIC_API_KEY: "inherited-api-key",
        CLAUDE_CODE_USE_BEDROCK: "true",
        CLAUDE_CODE_USE_VERTEX: "true",
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "999999",
        PATH: "/usr/bin",
      },
      {
        task_type: "montage",
        task_id: "task-1",
        execution_id: "execution-1",
        env: {
          ANBAN_API_KEY: "montage-override",
          ANBAN_API_URL: "https://montage.invalid",
          ANBAN_EXECUTION_TOKEN: "secret-execution-jwt",
          ANBAN_DEFAULT_PROJECT: "montage-project",
          ANTHROPIC_AUTH_TOKEN: "montage-token",
          ANTHROPIC_BASE_URL: "https://montage.invalid/anthropic",
          ANTHROPIC_MODEL: "montage-model",
          CLAUDE_CODE_DISABLE_THINKING: "true",
        },
        project_id: "project-1",
        execution_profile: {
          envs: {
            ANTHROPIC_AUTH_TOKEN: "runtime-token",
            ANTHROPIC_BASE_URL: "https://runtime.example.com/anthropic",
            ANTHROPIC_MODEL: "runtime-model",
          },
        },
      },
      "https://server.example.com",
      "execution-jwt",
    );
    expect(managed).toEqual(expect.objectContaining({
      ANBAN_DEFAULT_PROJECT: "project-1",
      ANBAN_TASK_ID: "task-1",
      ANBAN_EXECUTION_ID: "execution-1",
      ANBAN_TASK_TYPE: "montage",
      ANTHROPIC_AUTH_TOKEN: "runtime-token",
      ANTHROPIC_BASE_URL: "https://runtime.example.com/anthropic",
      ANTHROPIC_MODEL: "runtime-model",
      PATH: "/usr/bin",
    }));
    expect(managed).not.toHaveProperty("ANBAN_API_KEY");
    expect(managed).not.toHaveProperty("ANBAN_API_URL");
    expect(managed).not.toHaveProperty("ANBAN_EXECUTION_TOKEN");
    const isolated = buildExecutionEnvironment(
      { ANTHROPIC_API_KEY: "inherited-api-key", CLAUDE_CODE_USE_BEDROCK: "true", CLAUDE_CODE_USE_VERTEX: "true" },
      { task_type: "article", task_id: "task-1", execution_id: "execution-1", project_id: "project-1", execution_profile: { envs: { ANTHROPIC_MODEL: "runtime-model" } } },
      "https://server.example.com",
      "execution-jwt",
    );
    expect(isolated).not.toHaveProperty("ANTHROPIC_API_KEY");
    expect(isolated).not.toHaveProperty("CLAUDE_CODE_USE_BEDROCK");
    expect(isolated).not.toHaveProperty("CLAUDE_CODE_USE_VERTEX");
    const environment = buildExecutionEnvironment(
      { CLAUDE_CODE_MAX_OUTPUT_TOKENS: "999999" },
      {
        task_type: "montage",
        env: { CLAUDE_CODE_DISABLE_THINKING: "true" },
        task_id: "task-1",
        execution_id: "execution-1",
        project_id: "project-1",
        execution_profile: { envs: { ANTHROPIC_MODEL: "runtime-model" } },
      },
      "https://server.example.com",
      "execution-jwt",
    );
    expect(environment).not.toHaveProperty("CLAUDE_CODE_MAX_OUTPUT_TOKENS");
    expect(environment).not.toHaveProperty("CLAUDE_CODE_DISABLE_THINKING");
  });

  test("removes pinned SDK credential, provider routing, and model inputs inherited from the host", () => {
    const inherited = Object.fromEntries([
      "CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL",
      "CLAUDE_CODE_OAUTH_TOKEN",
      "CLAUDE_CODE_SESSION_ACCESS_TOKEN",
      "CLAUDE_CODE_HOST_AUTH_ENV_VAR",
      "CLAUDE_CODE_HOST_CREDS_FILE",
      "CLAUDE_CODE_SDK_HAS_HOST_AUTH_REFRESH",
      "CLAUDE_CODE_SKIP_BEDROCK_AUTH",
      "CLAUDE_CODE_SKIP_VERTEX_AUTH",
      "CLAUDE_CODE_SKIP_FOUNDRY_AUTH",
      "CLAUDE_CODE_SKIP_ANTHROPIC_AWS_AUTH",
      "CLAUDE_CODE_SKIP_ANTHROPIC_GOOGLE_CLOUD_AUTH",
      "CLAUDE_CODE_SKIP_MANTLE_AUTH",
      "ANTHROPIC_PROFILE",
      "ANTHROPIC_CONFIG_DIR",
      "ANTHROPIC_SCOPE",
      "ANTHROPIC_IDENTITY_TOKEN",
      "ANTHROPIC_IDENTITY_TOKEN_FILE",
      "ANTHROPIC_AWS_API_KEY",
      "ANTHROPIC_FOUNDRY_API_KEY",
      "ANTHROPIC_FOUNDRY_AUTH_TOKEN",
      "AWS_BEARER_TOKEN_BEDROCK",
      "AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY",
      "AWS_SESSION_TOKEN",
      "AWS_SHARED_CREDENTIALS_FILE",
      "AWS_CONTAINER_AUTHORIZATION_TOKEN",
      "GOOGLE_APPLICATION_CREDENTIALS",
      "GOOGLE_CLOUD_QUOTA_PROJECT",
      "GCE_METADATA_HOST",
      "ANTHROPIC_SMALL_FAST_MODEL",
      "ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION",
      "CLAUDE_CONFIG_DIR",
      "CLAUDE_SECURESTORAGE_CONFIG_DIR",
      "CLAUDE_CODE_REMOTE_SETTINGS_PATH",
      "CLAUDE_CODE_REMOTE_SETTINGS_POLL_MS",
      "CLAUDE_CODE_MOCK_REMOTE_SETTINGS",
    ].map((key) => [key, "host-value"]));

    const environment = buildExecutionEnvironment(
      inherited,
      { task_type: "article", task_id: "task-1", execution_id: "execution-1", project_id: "project-1", execution_profile: { envs: { ANTHROPIC_MODEL: "frozen-model" } } },
      "https://server.example.com",
      "execution-jwt",
    );

    for (const key of Object.keys(inherited)) expect(environment).not.toHaveProperty(key);
    expect(environment.ANTHROPIC_MODEL).toBe("frozen-model");
  });
});

describe("runnerLogLine", () => {
  test("trims and keeps user-facing log lines", () => {
    expect(runner.runnerLogLine("  正在生成封面图  ")).toBe("正在生成封面图");
  });

  test("drops blank lines", () => {
    expect(runner.runnerLogLine("  ")).toBeUndefined();
    expect(runner.runnerLogLine("\n")).toBeUndefined();
  });

  test("drops internal Claude Code diagnostics that leak model routing", () => {
    const line = "[claude-code:unrecognized_model] {\"model\":\"deepseek-flash[1m]\",\"query_source\":\"sdk\"}";
    expect(runner.runnerLogLine(line)).toBeUndefined();
    expect(runner.runnerLogLine("[claude-code:something] internal")).toBeUndefined();
  });

  test("keeps non-diagnostic stderr narration", () => {
    expect(runner.runnerLogLine("Reading reference asset …")).toBe("Reading reference asset …");
  });
});
