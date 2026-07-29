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
    profile_id: "balanced",
    provider: "volcengine_ark",
    model_id: "doubao-seed-evolving",
    protocol: "anthropic",
    context_window: 128000,
    reasoning_effort: "medium",
    thinking_required: false,
    display_name: "平衡型",
    runtime_env: {
      ANTHROPIC_BASE_URL: "https://ark.example.com/api/compatible",
      ANTHROPIC_AUTH_TOKEN: "secret",
      ANTHROPIC_MODEL: "doubao-seed-evolving",
    },
    model_usage_aliases: {
      "doubao-seed-evolving": { provider: "volcengine_ark", model: "doubao-seed-evolving" },
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

  test("rejects the obsolete top-level runtime model contract", () => {
    const { execution_profile: _profile, ...response } = validResponse();
    const legacy = {
      ...response,
      model: "doubao-seed-evolving",
      runtime_env: {},
      model_usage_aliases: {
        "doubao-seed-evolving": { provider: "volcengine_ark", model: "doubao-seed-evolving" },
      },
    };
    expect(() => validateBootstrapResponse("execution-1", legacy)).toThrow("execution profile");
  });

  test("rejects an execution profile whose fixed identity tuple drifts", () => {
    const response = validResponse();
    response.execution_profile.provider = "deepseek";
    response.execution_profile.model_usage_aliases["doubao-seed-evolving"].provider = "deepseek";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("execution profile identity");
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

  test("rejects a runtime model that differs from the canonical model", () => {
    const response = validResponse();
    response.execution_profile.runtime_env.ANTHROPIC_MODEL = "different-model";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("runtime model");
  });

  for (const testCase of [
    { name: "protocol", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.protocol = "openai"; }, message: "protocol" },
    { name: "context window", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.context_window = -1; }, message: "context window" },
    { name: "reasoning effort", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.reasoning_effort = "extreme"; }, message: "reasoning effort" },
    { name: "thinking requirement", mutate: (response: ReturnType<typeof validResponse>) => { response.execution_profile.reasoning_effort = ""; response.execution_profile.thinking_required = true; }, message: "thinking requirement" },
  ]) {
    test(`rejects an invalid execution profile ${testCase.name}`, () => {
      const response = validResponse();
      testCase.mutate(response);
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow(testCase.message);
    });
  }

  test("requires the canonical model usage alias", () => {
    const response = validResponse();
    delete response.execution_profile.model_usage_aliases[response.execution_profile.model_id];
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });

  test("rejects a model usage alias with a different identity", () => {
    const response = validResponse();
    response.execution_profile.model_usage_aliases[response.execution_profile.model_id].model = "different-model";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("model usage aliases");
  });
});
