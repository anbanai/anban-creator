import { describe, expect, test } from "bun:test";

import * as runner from "../src/runner.js";

const { terminalModelUsage, validateManagedInit } = runner;

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
