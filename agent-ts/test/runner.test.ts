import { describe, expect, test } from "bun:test";

import * as runner from "../src/runner.js";

const { buildExecutionEnvironment, terminalModelUsage, validateManagedInit } = runner;

describe("validateManagedInit", () => {
  test("requires the configured remote MCP and article plugin skill", () => {
    expect(() => validateManagedInit({ type: "system", subtype: "init", mcp_servers: [{ name: "anban", status: "connected" }], plugins: [{ name: "anban", path: "/anbanai" }], skills: [] }, "article")).toThrow("anban:humanizer");
  });

  test("maps terminal SDK usage through the bootstrap model aliases", () => {
    expect(terminalModelUsage({ raw: { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 } }, { raw: { provider: "anthropic", model: "claude-sonnet" } })).toEqual({ usage: [{ provider: "anthropic", model: "claude-sonnet", input_tokens: 3, output_tokens: 5, cache_read_input_tokens: 7, cache_creation_input_tokens: 11 }], cost_status: "reconciled", cost_diagnostics: [] });
  });
});

describe("executionReasoningOptions", () => {
  test("enforces the frozen maximum-quality reasoning controls", () => {
    const buildOptions = (runner as typeof runner & {
      executionReasoningOptions?: (profile: { reasoning_effort: string; thinking_required: boolean }) => unknown;
    }).executionReasoningOptions;

    expect(buildOptions?.({ reasoning_effort: "high", thinking_required: true })).toEqual({
      effort: "high",
      thinking: { type: "adaptive" },
    });
  });

  test("does not force reasoning controls when the frozen profile omits them", () => {
    const buildOptions = (runner as typeof runner & {
      executionReasoningOptions?: (profile: { reasoning_effort: string; thinking_required: boolean }) => unknown;
    }).executionReasoningOptions;

    expect(buildOptions?.({ reasoning_effort: "", thinking_required: false })).toEqual({});
  });
});

describe("buildExecutionEnvironment", () => {
  test("keeps frozen profile and execution identity ahead of conflicting Montage environment", () => {
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
