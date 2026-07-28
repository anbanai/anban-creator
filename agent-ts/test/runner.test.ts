import { describe, expect, test } from "bun:test";

import { terminalModelUsage, validateManagedInit } from "../src/runner.js";

describe("validateManagedInit", () => {
  test("requires the configured remote MCP and article plugin skill", () => {
    expect(() => validateManagedInit({ type: "system", subtype: "init", mcp_servers: [{ name: "anban", status: "connected" }], plugins: [{ name: "anban", path: "/anbanai" }], skills: [] }, "article")).toThrow("anban:humanizer");
  });

  test("maps terminal SDK usage through the bootstrap model aliases", () => {
    expect(terminalModelUsage({ raw: { inputTokens: 3, outputTokens: 5, cacheReadInputTokens: 7, cacheCreationInputTokens: 11 } }, { raw: { provider: "anthropic", model: "claude-sonnet" } })).toEqual({ usage: [{ provider: "anthropic", model: "claude-sonnet", input_tokens: 3, output_tokens: 5, cache_read_input_tokens: 7, cache_creation_input_tokens: 11 }], cost_status: "reconciled", cost_diagnostics: [] });
  });
});
