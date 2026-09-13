import { describe, expect, test } from "bun:test";

import { readFile } from "node:fs/promises";

import { bootstrap, executionProfileFingerprint, resolveAgentPackForTaskType, validateAgentPackCatalog, validateBootstrapResponse, type AgentPackCatalog, type BootstrapResponse } from "../src/bootstrap.js";
import type { JobConfig } from "../src/config.js";

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

const validResponse = (): BootstrapResponse => {
  const response: BootstrapResponse = {
    execution_token: tokenFor({ execution_id: "execution-1", task_id: "task-1", project_id: "project-1" }),
    execution_id: "execution-1",
    task_id: "task-1",
    task_type: "article",
    agent_pack_id: "article",
    agent_pack_version: "1.0.0",
    agent_pack_digest: "a".repeat(64),
    runtime_profile: "article",
    runtime_adapter: "standard",
    project_id: "project-1",
    prompt: "Write an article",
    execution_profile: {
      profile_id: "quality",
      provider: "moonshot",
      protocol: "anthropic",
      display_name: "极致<&>效果",
      profile_fingerprint: "",
      envs: validProfileEnvs(),
      model_usage_aliases: {
        "kimi-k3[1m]": { provider: "moonshot", model: "kimi-k3" },
      },
    },
    max_turns: 10,
    agent_flag: "anban:article",
    auto_memory_directory: ".claude/memory",
    artifact_transport: { mode: "stream" },
  };
  response.execution_profile.profile_fingerprint = executionProfileFingerprint(response.execution_profile);
  return response;
};

describe("validateBootstrapResponse", () => {
  test("rejects a response with a missing execution ID", () => {
    const response = validResponse() as Record<string, unknown>;
    delete response.execution_id;
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("execution identity is incomplete");
  });

  test("rejects a response with a mismatched execution ID", () => {
    const response = validResponse();
    response.execution_id = "execution-2";
    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("execution identity mismatch");
  });

  test("sends the runtime contract version header during bootstrap", async () => {
    const originalFetch = globalThis.fetch;
    let request: Request | undefined;
    globalThis.fetch = (async (input, init) => {
      request = new Request(input, init);
      return new Response("", { status: 503 });
    }) as typeof fetch;
    try {
      const config: JobConfig = {
        serverURL: "https://creator.example.test",
        executionID: "execution-1",
        workspace: "/workspace",
        workloadTokenFile: "/token",
        allowHTTPServer: false,
      };
      await expect(bootstrap(config, "workload-token")).rejects.toThrow("HTTP 503");
      expect(request?.headers.get("X-Anban-Agent-Contract-Version")).toBe("1");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("preserves the server error code when bootstrap rejects the runtime contract", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () => new Response(JSON.stringify({
      code: 42600,
      msg: "agent runtime contract upgrade required",
      error_code: "agent_runtime_upgrade_required",
    }), { status: 426, headers: { "Content-Type": "application/json" } })) as typeof fetch;
    try {
      const config: JobConfig = {
        serverURL: "https://creator.example.test",
        executionID: "execution-1",
        workspace: "/workspace",
        workloadTokenFile: "/token",
        allowHTTPServer: false,
      };
      await expect(bootstrap(config, "workload-token")).rejects.toThrow(
        "bootstrap returned HTTP 426: agent_runtime_upgrade_required: agent runtime contract upgrade required",
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("accepts and validates the frozen Agent Pack identity against the generated Catalog", async () => {
    const response = validResponse();
    const catalog = JSON.parse(await readFile(new URL("../../harness/agent-pack-catalog.json", import.meta.url), "utf8")) as AgentPackCatalog;
    const article = catalog.packs.find((pack) => pack.id === "article");
    if (!article) throw new Error("article Agent Pack is missing");
    response.agent_pack_version = article.version;
    response.agent_pack_digest = article.digest;

    expect(validateAgentPackCatalog(validateBootstrapResponse("execution-1", response), catalog).id).toBe("article");
  });

  test("rejects a frozen runtime profile that differs from the generated Catalog", () => {
    const response = validResponse();
    response.runtime_profile = "montage";
    const catalog: AgentPackCatalog = { packs: [{
      id: "article", version: "1.0.0", digest: "a".repeat(64),
      agent: { name: "article" }, bindings: { task_types: ["article"] },
      runtime: { profile: "article", adapter: "standard" },
      progress: [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100 }],
    }] };

    expect(() => validateAgentPackCatalog(validateBootstrapResponse("execution-1", response), catalog)).toThrow("Agent Pack identity");
  });

  test("rejects malformed Agent Pack progress contracts", () => {
    const response = validateBootstrapResponse("execution-1", validResponse());
    const basePack = {
      id: "article", version: "1.0.0", digest: "a".repeat(64),
      agent: { name: "article" }, bindings: { task_types: ["article"] },
      runtime: { profile: "article", adapter: "standard" },
    };
    const invalidProgress = [
      undefined,
      [{ id: "", title: "Research", active_percent: 10, complete_percent: 100 }],
      [{ id: "research", title: "", active_percent: 10, complete_percent: 100 }],
      [{ id: "research", title: "Research", active_percent: 10.5, complete_percent: 100 }],
      [{ id: "research", title: "Research", active_percent: -1, complete_percent: 100 }],
      [{ id: "research", title: "Research", active_percent: 30, complete_percent: 20 }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 101 }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: "output/a.md" }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: [1] }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 90 }],
      [
        { id: "research", title: "Research", active_percent: 10, complete_percent: 60 },
        { id: "writing", title: "Writing", active_percent: 20, complete_percent: 50 },
        { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100 },
      ],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: ["/tmp/a.md"] }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: ["../a.md"] }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: ["output/tmp/../a.md"] }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: ["output//a.md"] }],
      [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100, required_artifacts: ["output\\a.md"] }],
    ];

    for (const progress of invalidProgress) {
      const catalog = { packs: [{ ...basePack, ...(progress === undefined ? {} : { progress }) }] } as unknown as AgentPackCatalog;
      expect(() => validateAgentPackCatalog(response, catalog)).toThrow("progress");
    }
  });

  test("resolves a task-specific progress contract without mutating the Catalog Pack", () => {
    const response = validateBootstrapResponse("execution-1", {
      ...validResponse(),
      task_type: "viral_analysis",
      agent_pack_id: "seednote",
      runtime_profile: "seednote",
      agent_flag: "anban:seednote",
    });
    const defaultProgress = [
      { id: "research", title: "Research", active_percent: 5, complete_percent: 25 },
      { id: "writing", title: "Writing", active_percent: 35, complete_percent: 80 },
      { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100, required_artifacts: ["output/content.md", "output/image-plan.md"] },
    ];
    const viralProgress = [
      { id: "research", title: "Research", active_percent: 5, complete_percent: 40 },
      { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100, required_artifacts: ["output/source-analysis.md", "output/viral-template.json"] },
    ];
    const pack = {
      id: "seednote", version: "1.0.0", digest: "a".repeat(64),
      agent: { name: "seednote" }, bindings: { task_types: ["seednote", "viral_analysis"] },
      runtime: { profile: "seednote", adapter: "standard" },
      progress: defaultProgress,
      progress_by_task_type: { viral_analysis: viralProgress },
    };

    const resolved = validateAgentPackCatalog(response, { packs: [pack] } as unknown as AgentPackCatalog);

    expect(resolved).not.toBe(pack);
    expect(resolved.progress).toEqual(viralProgress);
    expect(pack.progress).toBe(defaultProgress);

    resolved.progress[0]!.title = "Changed";
    resolved.progress[1]!.required_artifacts!.push("output/changed.json");
    expect(viralProgress[0]!.title).toBe("Research");
    expect(viralProgress[1]!.required_artifacts).toEqual(["output/source-analysis.md", "output/viral-template.json"]);
  });

  test("shared progress resolution selects overrides and falls back to an isolated default", () => {
    const pack = {
      id: "seednote", version: "1.0.0", digest: "a".repeat(64),
      agent: { name: "seednote" }, bindings: { task_types: ["seednote", "viral_analysis"] },
      runtime: { profile: "seednote", adapter: "standard" },
      progress: [
        { id: "research", title: "Research", active_percent: 5, complete_percent: 25 },
        { id: "writing", title: "Writing", active_percent: 35, complete_percent: 80 },
        { id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100 },
      ],
      progress_by_task_type: {
        viral_analysis: [
          { id: "research", title: "Viral Research", active_percent: 5, complete_percent: 80 },
          { id: "delivery", title: "Viral Delivery", active_percent: 90, complete_percent: 100 },
        ],
      },
    } as AgentPackCatalog["packs"][number];

    expect(resolveAgentPackForTaskType(pack, "viral_analysis").progress.map((stage) => stage.id)).toEqual(["research", "delivery"]);
    const fallback = resolveAgentPackForTaskType(pack, "seednote");
    expect(fallback.progress.map((stage) => stage.id)).toEqual(["research", "writing", "delivery"]);
    expect(fallback.progress).not.toBe(pack.progress);
  });

  test("rejects malformed task-specific progress contracts", () => {
    const response = validateBootstrapResponse("execution-1", validResponse());
    const basePack = {
      id: "article", version: "1.0.0", digest: "a".repeat(64),
      agent: { name: "article" }, bindings: { task_types: ["article"] },
      runtime: { profile: "article", adapter: "standard" },
      progress: [{ id: "research", title: "Research", active_percent: 10, complete_percent: 100 }],
    };
    const invalidOverrides = [
      [],
      { unknown: [{ id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100 }] },
      { article: [] },
      { article: [
        { id: "research", title: "Research", active_percent: 10, complete_percent: 60 },
        { id: "delivery", title: "Delivery", active_percent: 40, complete_percent: 100 },
      ] },
      { article: [{ id: "delivery", title: "Delivery", active_percent: 90, complete_percent: 100, required_artifacts: ["../secret.txt"] }] },
    ];

    for (const progress_by_task_type of invalidOverrides) {
      const catalog = { packs: [{ ...basePack, progress_by_task_type }] } as unknown as AgentPackCatalog;
      expect(() => validateAgentPackCatalog(response, catalog)).toThrow("progress");
    }
  });

  test("accepts all Claude profile env values without rewriting them", () => {
    const response = validResponse();
    expect(validateBootstrapResponse("execution-1", response).execution_profile.envs).toEqual(response.execution_profile.envs);
  });

  test("matches the Server fingerprint canonicalization vector", () => {
    expect(executionProfileFingerprint(validResponse().execution_profile)).toBe("ddeb3859ae9f15f7fd674caa0982326d05cce496d2426a8be892845643d31aa7");
  });

  test("preserves the current auth token bytes", () => {
    const response = validResponse();
    response.execution_profile.envs.ANTHROPIC_AUTH_TOKEN = " rotated-secret ";

    expect(validateBootstrapResponse("execution-1", response).execution_profile.envs.ANTHROPIC_AUTH_TOKEN).toBe(" rotated-secret ");
  });

  test("accepts only the three new execution profile IDs", () => {
    for (const profileID of ["effective", "balanced", "quality"]) {
      const response = validResponse();
      response.execution_profile.profile_id = profileID as BootstrapResponse["execution_profile"]["profile_id"];
      response.execution_profile.profile_fingerprint = executionProfileFingerprint(response.execution_profile);
      expect(validateBootstrapResponse("execution-1", response).execution_profile.profile_id).toBe(profileID);
    }
    for (const profileID of ["cost_effective", "maximum_quality", "custom"]) {
      const response = validResponse();
      (response.execution_profile as unknown as Record<string, unknown>).profile_id = profileID;
      expect(() => validateBootstrapResponse("execution-1", response)).toThrow("identity");
    }
  });

  test("accepts viral analysis through the Seednote agent route", () => {
    const response = validResponse();
    response.task_type = "viral_analysis";
    response.agent_flag = "anban:seednote";

    expect(validateBootstrapResponse("execution-1", response).agent_flag).toBe("anban:seednote");
  });

  test("rejects a frozen profile whose fingerprint does not match its contents", () => {
    const response = validResponse();
    response.execution_profile.display_name = "tampered display name";

    expect(() => validateBootstrapResponse("execution-1", response)).toThrow("fingerprint does not match snapshot");
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
    response.execution_profile.profile_fingerprint = executionProfileFingerprint(response.execution_profile);
    expect(validateBootstrapResponse("execution-1", response).execution_profile.profile_id).toBe("quality");
  });

  test("validates the managed replacement flag", () => {
    const accepted = validResponse();
    accepted.files = [{ path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true }];
    expect(validateBootstrapResponse("execution-1", accepted).files?.[0]?.replace_existing).toBe(true);

    const rejected = validResponse();
    rejected.files = [{ path: ".anban-creator/settings.json", text: "new", mode: 0o600 }];
    (rejected.files[0] as unknown as Record<string, unknown>).replace_existing = "true";
    expect(() => validateBootstrapResponse("execution-1", rejected)).toThrow("replace_existing");
  });

  test("requires an execution-scoped resume context path", () => {
    const accepted = validResponse();
    accepted.resume_session_id = "session-1";
    accepted.resume_context_path = ".anban-creator/resume/executions/execution-1/latest.md";
    expect(validateBootstrapResponse("execution-1", accepted).resume_context_path).toBe(accepted.resume_context_path);

    const rejected = validResponse();
    rejected.resume_session_id = "session-1";
    rejected.resume_context_path = ".anban-creator/resume/executions/other/latest.md";
    expect(() => validateBootstrapResponse("execution-1", rejected)).toThrow("resume context path");
  });

  test("accepts Claude traffic and auto-memory controls", () => {
    const response = validResponse();
    response.execution_profile.envs.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1";
    response.execution_profile.envs.CLAUDE_CODE_DISABLE_AUTO_MEMORY = "0";
    response.execution_profile.profile_fingerprint = executionProfileFingerprint(response.execution_profile);

    expect(validateBootstrapResponse("execution-1", response).execution_profile.envs).toMatchObject({
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
      CLAUDE_CODE_DISABLE_AUTO_MEMORY: "0",
    });
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
    response.execution_profile.profile_fingerprint = executionProfileFingerprint(response.execution_profile);

    expect(() => validateBootstrapResponse("execution-1", response)).not.toThrow();
  });

  test("validates optional Claude env value domains", () => {
    for (const [key, value] of [
      ["CLAUDE_CODE_EFFORT_LEVEL", "extreme"],
      ["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "true"],
      ["CLAUDE_CODE_DISABLE_AUTO_MEMORY", "false"],
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
