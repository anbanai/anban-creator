import { describe, expect, test } from "bun:test";

import { validateBootstrapResponse } from "../src/bootstrap.js";

const tokenFor = (claims: Record<string, string>) => `header.${Buffer.from(JSON.stringify(claims)).toString("base64url")}.signature`;

const validResponse = () => ({
  execution_token: tokenFor({ execution_id: "execution-1", task_id: "task-1", project_id: "project-1" }),
  task_id: "task-1",
  task_type: "article",
  project_id: "project-1",
  prompt: "Write an article",
  execution_profile: {
    profile_id: "maximum_quality",
    provider: "moonshot",
    protocol: "anthropic",
    models: {
      default: "kimi-k3[1m]",
      opus: "kimi-k3[1m]",
      fable: "kimi-k3[1m]",
      sonnet: "kimi-k3[1m]",
      haiku: "kimi-k3[1m]",
    },
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
  test("accepts a response whose signed identity matches the requested execution", () => {
    expect(validateBootstrapResponse("execution-1", validResponse())).toMatchObject({ task_id: "task-1" });
  });

  test("rejects a bootstrap file that targets protected agent memory", () => {
    const response = validResponse();
    response.files = [{ path: ".claude/memory/session.md", text: "unsafe", mode: 0o600 }];
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("protected auto memory");
  });

  test("rejects identity claims for another execution", () => {
    const response = validResponse();
    response.execution_token = tokenFor({ execution_id: "other", task_id: "task-1", project_id: "project-1" });
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("identity mismatch");
  });

  for (const [field, value] of [
    ["model", "doubao-seed-evolving"],
    ["runtime_env", {}],
    ["model_usage_aliases", { "doubao-seed-evolving": { provider: "volcengine_ark", model: "doubao-seed-evolving" } }],
  ] as const) {
    test(`rejects the obsolete top-level ${field} field alongside an execution profile`, () => {
      const response = validResponse() as ReturnType<typeof validResponse> & Record<string, unknown>;
      response[field] = value;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("legacy");
    });
  }

  test("rejects unknown top-level response fields", () => {
    const response = validResponse() as ReturnType<typeof validResponse> & Record<string, unknown>;
    response.unexpected = true;
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("unknown fields");
  });

  test("accepts every stable execution profile ID and rejects another ID", () => {
    for (const profileID of ["cost_effective", "balanced", "maximum_quality"]) {
      const response = validResponse();
      response.execution_profile.profile_id = profileID;
      expect(validateBootstrapResponse("execution-1", response).execution_profile.profile_id).toBe(profileID);
    }

    const response = validResponse();
    response.execution_profile.profile_id = "custom";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("identity");
  });

  test("accepts an arbitrary provider and split model matrix when the structure agrees", () => {
    const response = validResponse();
    response.execution_profile.profile_id = "balanced";
    response.execution_profile.provider = "zhipu";
    response.execution_profile.models = {
      default: "glm-5.2",
      opus: "glm-5.2",
      fable: "glm-5.2",
      sonnet: "glm-5.2-air",
      haiku: "glm-5.2-air",
    };
    response.execution_profile.runtime_env.ANTHROPIC_BASE_URL = "https://open.bigmodel.cn/api/anthropic";
    response.execution_profile.runtime_env.ANTHROPIC_MODEL = "glm-5.2";
    response.execution_profile.runtime_env.ANTHROPIC_DEFAULT_OPUS_MODEL = "glm-5.2";
    response.execution_profile.runtime_env.ANTHROPIC_DEFAULT_FABLE_MODEL = "glm-5.2";
    response.execution_profile.runtime_env.ANTHROPIC_DEFAULT_SONNET_MODEL = "glm-5.2-air";
    response.execution_profile.runtime_env.ANTHROPIC_DEFAULT_HAIKU_MODEL = "glm-5.2-air";
    response.execution_profile.model_usage_aliases = {
      "glm-5.2": { provider: "zhipu", model: "glm-5.2" },
      "glm-5.2-air": { provider: "zhipu", model: "glm-5.2-air" },
    };
    expect(validateBootstrapResponse("execution-1", response).execution_profile.provider).toBe("zhipu");
  });

  test("rejects a non-HTTPS provider base URL", () => {
    const response = validResponse();
    response.execution_profile.runtime_env.ANTHROPIC_BASE_URL = "http://ark.example.com/api/compatible";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime environment");
  });

  test("rejects a missing provider auth token", () => {
    const response = validResponse();
    delete response.execution_profile.runtime_env.ANTHROPIC_AUTH_TOKEN;
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime environment");
  });

  test("rejects runtime environment keys outside the server allowlist", () => {
    const response = validResponse();
    response.execution_profile.runtime_env.PATH = "/tmp/bin";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime environment");
  });

  test("rejects a runtime role model that differs from the frozen matrix", () => {
    const response = validResponse();
    response.execution_profile.runtime_env.ANTHROPIC_MODEL = "different-model";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime model");
  });

  for (const testCase of [
    { name: "protocol", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.protocol = "openai"; }, message: "protocol" },
    { name: "fingerprint", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.profile_fingerprint = "ABC"; }, message: "fingerprint" },
    { name: "matrix role", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.models.haiku = ""; }, message: "model matrix" },
  ]) {
    test(`rejects an invalid execution profile ${testCase.name}`, () => {
      const response = validResponse();
      testCase.mutate(response);
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow(testCase.message);
    });
  }

  test("requires an alias for every distinct matrix model", () => {
    const response = validResponse();
    delete response.execution_profile.model_usage_aliases[response.execution_profile.models.default];
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  test("rejects a model usage alias with a different identity", () => {
    const response = validResponse();
    response.execution_profile.model_usage_aliases[response.execution_profile.models.default].provider = "other";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  for (const [field, value] of [
    ["model_id", "kimi-k3[1m]"],
    ["context_window", 1_000_000],
    ["reasoning_effort", "high"],
    ["thinking_required", true],
  ] as const) {
    test(`rejects the removed execution profile ${field} field`, () => {
      const response = validResponse();
      (response.execution_profile as unknown as Record<string, unknown>)[field] = value;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("legacy");
    });
  }


  test("rejects unknown execution profile fields", () => {
    const response = validResponse();
    (response.execution_profile as unknown as Record<string, unknown>).auth_token = "must-not-be-accepted";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("unknown fields");
  });

  test("rejects unknown model usage alias identity fields", () => {
    const response = validResponse();
    (response.execution_profile.model_usage_aliases["kimi-k3[1m]"] as Record<string, unknown>).endpoint = "https://example.invalid";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  test("accepts all Claude controls when their runtime environment values match exactly", () => {
    const response = validResponse();
    response.execution_profile.claude = {
      effort_level: "max",
      always_enable_effort: true,
      max_context_tokens: 1_000_000,
      max_output_tokens: 64_000,
      max_thinking_tokens: 0,
      disable_adaptive_thinking: false,
      disable_thinking: true,
      auto_compact_window: 900_000,
      autocompact_pct_override: 85,
      disable_1m_context: false,
      subagent_model: "kimi-k2.7-code",
      enable_tool_search: false,
    };
    Object.assign(response.execution_profile.runtime_env, {
      CLAUDE_CODE_EFFORT_LEVEL: "max",
      CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "true",
      CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1000000",
      CLAUDE_CODE_MAX_OUTPUT_TOKENS: "64000",
      MAX_THINKING_TOKENS: "0",
      CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false",
      CLAUDE_CODE_DISABLE_THINKING: "true",
      CLAUDE_CODE_AUTO_COMPACT_WINDOW: "900000",
      CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "85",
      CLAUDE_CODE_DISABLE_1M_CONTEXT: "false",
      CLAUDE_CODE_SUBAGENT_MODEL: "kimi-k2.7-code",
      ENABLE_TOOL_SEARCH: "false",
    });
    response.execution_profile.model_usage_aliases["kimi-k2.7-code"] = { provider: "moonshot", model: "kimi-k2.7-code" };

    expect(validateBootstrapResponse("execution-1", response).execution_profile.claude).toEqual(response.execution_profile.claude);
  });

  for (const [control, envKey, wrongValue] of [
    ["max_thinking_tokens", "MAX_THINKING_TOKENS", "1"],
    ["enable_tool_search", "ENABLE_TOOL_SEARCH", "true"],
  ] as const) {
    test(`requires an exact runtime environment value for explicit ${control}`, () => {
      const response = validResponse();
      response.execution_profile.runtime_env[envKey] = wrongValue;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime environment");
    });
  }

  test("requires a model usage alias for the configured subagent model", () => {
    const response = validResponse();
    response.execution_profile.claude.subagent_model = "kimi-k2.7-code";
    response.execution_profile.runtime_env.CLAUDE_CODE_SUBAGENT_MODEL = "kimi-k2.7-code";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  test("rejects a control environment key that is not represented in claude controls", () => {
    const response = validResponse();
    response.execution_profile.runtime_env.CLAUDE_CODE_MAX_OUTPUT_TOKENS = "4096";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime environment");
  });
});
