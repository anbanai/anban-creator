import { describe, expect, test } from "bun:test";

import { validateBootstrapResponse } from "../src/bootstrap.js";

const tokenFor = (claims: Record<string, string>) => `header.${Buffer.from(JSON.stringify(claims)).toString("base64url")}.signature`;

const validResponse = () => ({
  execution_token: tokenFor({ execution_id: "execution-1", task_id: "task-1", project_id: "project-1" }),
  task_id: "task-1",
  task_type: "article",
  project_id: "project-1",
  prompt: "Write an article",
  model: "claude-sonnet-4-5",
  max_turns: 10,
  agent_flag: "anban:article",
  auto_memory_directory: ".claude/memory",
  runtime_env: {},
  model_usage_aliases: { "claude-sonnet-4-5": { provider: "anthropic", model: "claude-sonnet-4-5" } },
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
});
