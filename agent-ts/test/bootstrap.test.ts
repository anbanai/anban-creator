import { describe, expect, test } from "bun:test";

import { validateBootstrapResponse } from "../src/bootstrap.js";

const tokenFor = (claims: Record<string, string>) => `header.${Buffer.from(JSON.stringify(claims)).toString("base64url")}.signature`;

const validProfileEnvs = () => ({
  ANTHROPIC_BASE_URL: "https://api.moonshot.cn/anthropic",
  ANTHROPIC_AUTH_TOKEN: "secret",
  ANTHROPIC_MODEL: "kimi-k3[1m]",
  ANTHROPIC_DEFAULT_OPUS_MODEL: "kimi-k3[1m]",
  ANTHROPIC_DEFAULT_FABLE_MODEL: "kimi-k3[1m]",
  ANTHROPIC_DEFAULT_SONNET_MODEL: "kimi-k3[1m]",
  ANTHROPIC_DEFAULT_HAIKU_MODEL: "kimi-k3[1m]",
  CLAUDE_CODE_EFFORT_LEVEL: "high",
  CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "true",
  CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1048576",
  CLAUDE_CODE_MAX_OUTPUT_TOKENS: "131072",
  MAX_THINKING_TOKENS: "0",
  CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false",
  CLAUDE_CODE_DISABLE_THINKING: "false",
  CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1048576",
  CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "80",
  CLAUDE_CODE_DISABLE_1M_CONTEXT: "false",
  CLAUDE_CODE_SUBAGENT_MODEL: "kimi-k3[1m]",
  ENABLE_TOOL_SEARCH: "true",
});

const validResponse = () => ({
  execution_token: tokenFor({ execution_id: "execution-1", task_id: "task-1", project_id: "project-1" }),
  task_id: "task-1",
  task_type: "article",
  project_id: "project-1",
  prompt: "Write an article",
  execution_profile: {
    profile_id: "quality",
    provider: "moonshot",
    protocol: "anthropic",
    display_name: "极致效果",
    profile_fingerprint: "a".repeat(64),
    envs: validProfileEnvs(),
    model_usage_aliases: {
      "kimi-k3[1m]": { provider: "moonshot", model: "kimi-k3" },
    },
  },
  max_turns: 10,
  agent_flag: "anban:article",
  auto_memory_directory: ".claude/memory",
  artifact_transport: { mode: "stream" },
});

describe("validateBootstrapResponse", () => {
  test("accepts all 19 Claude profile env values without rewriting them", () => {
    const response = validResponse();
    expect(validateBootstrapResponse("execution-1", response).execution_profile.envs).toEqual(response.execution_profile.envs);
  });

  test("preserves the current auth token bytes", () => {
    const response = validResponse();
    response.execution_profile.envs.ANTHROPIC_AUTH_TOKEN = " rotated-secret ";

    expect(validateBootstrapResponse("execution-1", response).execution_profile.envs.ANTHROPIC_AUTH_TOKEN).toBe(" rotated-secret ");
  });

  test("accepts only the three new execution profile IDs", () => {
    for (const profileID of ["effective", "balanced", "quality"]) {
      const response = validResponse();
      response.execution_profile.profile_id = profileID;
      expect(validateBootstrapResponse("execution-1", response).execution_profile.profile_id).toBe(profileID);
    }
    for (const profileID of ["cost_effective", "maximum_quality", "custom"]) {
      const response = validResponse();
      response.execution_profile.profile_id = profileID;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("identity");
    }
  });

  test("rejects legacy profile payload fields", () => {
    for (const field of ["models", "claude", "runtime_env"]) {
      const response = validResponse();
      (response.execution_profile as unknown as Record<string, unknown>)[field] = {};
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("unknown fields");
    }
  });

  test("rejects unknown envs and missing auth", () => {
    const unknown = validResponse();
    (unknown.execution_profile.envs as Record<string, string>).PATH = "/tmp";
    expect(() => validateBootstrapResponse("execution-1", unknown)).toThrow("environment");

    const missing = validResponse();
    delete (missing.execution_profile.envs as Partial<Record<string, string>>).ANTHROPIC_AUTH_TOKEN;
    expect(() => validateBootstrapResponse("execution-1", missing)).toThrow("environment");
  });

  test("accepts omitted optional controls", () => {
    const response = validResponse();
    for (const key of Object.keys(response.execution_profile.envs)) {
      if (!key.startsWith("ANTHROPIC_")) delete (response.execution_profile.envs as Record<string, string>)[key];
    }
    expect(validateBootstrapResponse("execution-1", response).execution_profile.profile_id).toBe("quality");
  });

  test("uses the Server value-only byte limit for Claude profile envs", () => {
    const response = validResponse();
    const model = "m".repeat(3324);
    response.execution_profile.envs = {
      ANTHROPIC_BASE_URL: "https://api.example.com/v1",
      ANTHROPIC_AUTH_TOKEN: "t".repeat(16000),
      ANTHROPIC_MODEL: model,
      ANTHROPIC_DEFAULT_OPUS_MODEL: model,
      ANTHROPIC_DEFAULT_FABLE_MODEL: model,
      ANTHROPIC_DEFAULT_SONNET_MODEL: model,
      ANTHROPIC_DEFAULT_HAIKU_MODEL: model,
    } as ReturnType<typeof validProfileEnvs>;
    response.execution_profile.model_usage_aliases = {
      [model]: { provider: "moonshot", model: "canonical-model" },
    } as typeof response.execution_profile.model_usage_aliases;

    expect(() => validateBootstrapResponse("execution-1", response)).not.toThrow();
  });

  test("validates optional Claude env value domains", () => {
    for (const [key, value] of [
      ["CLAUDE_CODE_EFFORT_LEVEL", "extreme"],
      ["CLAUDE_CODE_DISABLE_THINKING", "yes"],
      ["CLAUDE_CODE_MAX_CONTEXT_TOKENS", "0"],
      ["CLAUDE_CODE_MAX_CONTEXT_TOKENS", "0001"],
      ["MAX_THINKING_TOKENS", "-1"],
      ["MAX_THINKING_TOKENS", "00"],
      ["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "101"],
    ] as const) {
      const response = validResponse();
      response.execution_profile.envs[key] = value;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("environment");
    }
  });

  test("requires aliases for every referenced model including subagents", () => {
    const response = validResponse();
    response.execution_profile.envs.CLAUDE_CODE_SUBAGENT_MODEL = "kimi-k2.7-code";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  test("rejects non-HTTPS or credential-bearing provider URLs", () => {
    for (const baseURL of ["http://api.example.com/anthropic", "https://user:pass@api.example.com/anthropic", "https://api.example.com/anthropic?q=1"]) {
      const response = validResponse();
      response.execution_profile.envs.ANTHROPIC_BASE_URL = baseURL;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("environment");
    }
  });

  test("rejects unknown alias fields and provider mismatch", () => {
    const extra = validResponse();
    (extra.execution_profile.model_usage_aliases["kimi-k3[1m]"] as Record<string, unknown>).endpoint = "https://example.invalid";
    expect(() => validateBootstrapResponse("execution-1", extra)).toThrow("model usage aliases");

    const mismatch = validResponse();
    mismatch.execution_profile.model_usage_aliases["kimi-k3[1m]"].provider = "other";
    expect(() => validateBootstrapResponse("execution-1", mismatch)).toThrow("model usage aliases");
  });

  test("rejects identity claims for another execution", () => {
    const response = validResponse();
    response.execution_token = tokenFor({ execution_id: "other", task_id: "task-1", project_id: "project-1" });
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("identity mismatch");
  });

  test("rejects protected memory files and unknown response fields", () => {
    const fileResponse = validResponse();
    fileResponse.files = [{ path: ".claude/memory/session.md", text: "unsafe", mode: 0o600 }];
    expect(() => validateBootstrapResponse("execution-1", fileResponse)).toThrow("protected auto memory");

    const unknown = validResponse() as ReturnType<typeof validResponse> & Record<string, unknown>;
    unknown.unexpected = true;
    expect(() => validateBootstrapResponse("execution-1", unknown)).toThrow("unknown fields");
  });
});
