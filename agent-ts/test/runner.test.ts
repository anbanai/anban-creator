import { describe, expect, test } from "bun:test";

import * as runner from "../src/runner.js";

const { buildExecutionEnvironment, terminalModelUsage, validateManagedInit } = runner;

const validBootstrap = () => ({
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

describe("validateManagedInit", () => {
  test("requires the configured remote MCP and article plugin skill", () => {
    expect(() => validateManagedInit({ type: "system", subtype: "init", mcp_servers: [{ name: "anban", status: "connected" }], plugins: [{ name: "anban", path: "/anbanai" }], tools: [], skills: [] }, "article")).toThrow("anban:humanizer");
  });

  test("requires Seednote core tools and phase skills while research tools remain optional", () => {
    const message = {
      type: "system" as const,
      subtype: "init" as const,
      mcp_servers: [{ name: "anban", status: "connected" as const }],
      plugins: [{ name: "anban", path: "/anbanai" }],
      tools: [
        "mcp__anban__analyze_image",
        "mcp__anban__claim_topic",
        "mcp__anban__check_seednote_login_status",
        "mcp__anban__finalize_task_title",
        "mcp__anban__generate_image",
        "mcp__anban__get_project_profile",
        "mcp__anban__get_seednote_feed_detail",
        "mcp__anban__get_seednote_login_qrcode",
        "mcp__anban__get_seednote_user_profile",
        "mcp__anban__list_project_titles",
        "mcp__anban__search_seednote_feeds",
        "mcp__anban__submit_agent_feedback",
        "mcp__anban__update_task_progress",
      ],
      skills: [
        "anban:seednote-research",
        "anban:seednote-viral-analysis",
        "anban:seednote-writing",
        "anban:seednote-visual-design",
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
  });

  test("applies Seednote readiness to viral analysis without requiring external research tools", () => {
    const message = {
      type: "system" as const,
      subtype: "init" as const,
      mcp_servers: [{ name: "anban", status: "connected" as const }],
      plugins: [{ name: "anban", path: "/anbanai" }],
      tools: [
        "mcp__anban__get_project_profile",
        "mcp__anban__list_project_titles",
        "mcp__anban__submit_agent_feedback",
        "mcp__anban__update_task_progress",
      ],
      skills: [
        "anban:seednote-research",
        "anban:seednote-viral-analysis",
        "anban:seednote-writing",
        "anban:seednote-visual-design",
      ],
    };
    expect(() => validateManagedInit(message, "viral_analysis")).not.toThrow();
    message.skills = [];
    expect(() => validateManagedInit(message, "viral_analysis")).toThrow("anban:seednote-research");
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
  test("uses the frozen runtime adapter for the working directory", () => {
    const data = { ...validBootstrap(), task_type: "montage", runtime_adapter: "standard" };
    expect(runner.buildQueryOptions(data, "/workspace").cwd).toBe("/workspace");
    expect(runner.buildQueryOptions({ ...data, runtime_adapter: "openmontage" }, "/workspace").cwd).toBe("/workspace/openmontage");
  });

  test("keeps unrestricted access within the managed session policy", () => {
    const options = runner.buildQueryOptions(validBootstrap(), "/workspace");

    expect(options.permissionMode).toBe("bypassPermissions");
    expect(options.allowDangerouslySkipPermissions).toBe(true);
    expect(options).not.toHaveProperty("allowedTools");
    expect(options.disallowedTools).toEqual(["Agent", "ScheduleWakeup", "AskUserQuestion"]);
    expect(options).not.toHaveProperty("canUseTool");
  });

  test("enforces the MCP boundary and Seednote stop gate for both Seednote task types", async () => {
    const articleOptions = runner.buildQueryOptions(validBootstrap(), "/workspace");
    const articleHooks = articleOptions.hooks as Record<string, Array<{ hooks: Array<(input: unknown, toolUseID: string | undefined, options: { signal: AbortSignal }) => Promise<Record<string, unknown>>> }>>;
    expect(articleHooks.PreToolUse).toHaveLength(1);
    expect(articleHooks.Stop).toBeUndefined();

    const boundary = articleHooks.PreToolUse[0].hooks[0];
    const hookOptions = { signal: new AbortController().signal };
    const direct = await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "curl http://seednote:18060/mcp" }, tool_use_id: "tool-1" }, "tool-1", hookOptions);
    expect(direct).toMatchObject({ hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny" } });
    const directRest = await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "curl http://seednote:18060/api/v1/feeds" }, tool_use_id: "tool-rest" }, "tool-rest", hookOptions);
    expect(directRest).toMatchObject({ hookSpecificOutput: { permissionDecision: "deny" } });
    expect(await boundary({ hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: { command: "printf done" }, tool_use_id: "tool-2" }, "tool-2", hookOptions)).toEqual({});

    for (const taskType of ["seednote", "viral_analysis"]) {
      const options = runner.buildQueryOptions({ ...validBootstrap(), task_type: taskType }, "/workspace");
      const hooks = options.hooks as Record<string, unknown[]>;
      expect(hooks.PreToolUse).toHaveLength(1);
      expect(hooks.Stop).toHaveLength(1);
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
});

describe("buildExecutionEnvironment", () => {
  test("applies frozen runtime environment after execution identity", () => {
    expect(buildExecutionEnvironment(
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
        env: {
          NEW_PROVIDER_TOKEN: "future-secret",
          ANBAN_API_KEY: "montage-override",
          ANBAN_API_URL: "https://montage.invalid",
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
    )).toEqual(expect.objectContaining({
      NEW_PROVIDER_TOKEN: "future-secret",
      ANBAN_API_KEY: "execution-jwt",
      ANBAN_API_URL: "https://server.example.com",
      ANBAN_DEFAULT_PROJECT: "project-1",
      ANTHROPIC_AUTH_TOKEN: "runtime-token",
      ANTHROPIC_BASE_URL: "https://runtime.example.com/anthropic",
      ANTHROPIC_MODEL: "runtime-model",
      PATH: "/usr/bin",
    }));
    const isolated = buildExecutionEnvironment(
      { ANTHROPIC_API_KEY: "inherited-api-key", CLAUDE_CODE_USE_BEDROCK: "true", CLAUDE_CODE_USE_VERTEX: "true" },
      { task_type: "article", project_id: "project-1", execution_profile: { envs: { ANTHROPIC_MODEL: "runtime-model" } } },
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
      { task_type: "article", project_id: "project-1", execution_profile: { envs: { ANTHROPIC_MODEL: "frozen-model" } } },
      "https://server.example.com",
      "execution-jwt",
    );

    for (const key of Object.keys(inherited)) expect(environment).not.toHaveProperty(key);
    expect(environment.ANTHROPIC_MODEL).toBe("frozen-model");
  });
});
