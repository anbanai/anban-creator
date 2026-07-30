import { describe, expect, test } from "bun:test";

import * as runner from "../src/runner.js";

const { buildExecutionEnvironment, terminalModelUsage, validateManagedInit } = runner;

const validBootstrap = () => ({
  task_type: "article",
  project_id: "project-1",
  max_turns: 10,
  agent_flag: "anban:article",
  prompt: "write",
  artifact_transport: { mode: "stream" },
  execution_profile: {
    profile_id: "maximum_quality",
    provider: "moonshot",
    protocol: "anthropic",
    models: { default: "kimi-k3[1m]", opus: "kimi-k3[1m]", fable: "kimi-k3[1m]", sonnet: "kimi-k3[1m]", haiku: "kimi-k3[1m]" },
    claude: { max_thinking_tokens: 0, enable_tool_search: false },
    display_name: "极致效果",
    profile_fingerprint: "a".repeat(64),
    runtime_env: {
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
    expect(() => validateManagedInit({ type: "system", subtype: "init", mcp_servers: [{ name: "anban", status: "connected" }], plugins: [{ name: "anban", path: "/anbanai" }], skills: [] }, "article")).toThrow("anban:humanizer");
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

describe("buildExecutionEnvironment", () => {
  test("applies frozen runtime environment after execution identity", () => {
    expect(buildExecutionEnvironment(
      { ANTHROPIC_MODEL: "process-model" },
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
        },
        project_id: "project-1",
        execution_profile: {
          runtime_env: {
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
    }));
  });
});
