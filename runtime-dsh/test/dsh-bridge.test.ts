import { describe, expect, test } from "bun:test";
import {
  mapHarnessNotification,
  parseInteractiveCommand,
  redactInteractiveSecrets,
  resolveInteractivePatchPath,
  validateInteractivePresetId,
  assertInteractiveRuntimeCapabilities,
  DshBridge,
  parseInteractiveRouteArgs,
  writeDshRouteSettings,
} from "../src/dsh-bridge.js";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

describe("DSH bridge protocol", () => {
  test("refreshes interaction capability and filters stale generation events", async () => {
    expect(parseInteractiveCommand(JSON.stringify({ method: "interaction/capability-refresh", params: { interaction_id: "i-1", generation: 7 } }))).toMatchObject({ method: "interaction/capability-refresh" });
    const stale = mapHarnessNotification({ method: "session.status", params: { sessionId: "s", status: "running", generation: 6 } }, "s", { interaction_id: "i-1", generation: 7 });
    expect(stale).toEqual([]);
    const current = mapHarnessNotification({ method: "session.status", params: { sessionId: "s", status: "running", generation: 7 } }, "s", { interaction_id: "i-1", generation: 7 });
    expect(current).toEqual([{ event: "session.status", data: { session_id: "s", status: "running", generation: 7 } }]);
  });

  test("normalizes session errors only for interaction-scoped notifications", () => {
    const notification = { method: "session.event" as const, params: { sessionId: "s", generation: 2, event: { type: "tool/result", data: { message: { toolCallId: "c", isError: true, content: [{ type: "text", text: "interactive session not found" }] }, error: { code: "SESSION_NOT_FOUND" } } } } };
    expect(mapHarnessNotification(notification, "s", { interaction_id: "i-1", generation: 2 })[0]).toMatchObject({ data: { generation: 2, code: "assistant_interaction_restart_required", message: "the interaction can be restored" } });
  });

  test("accepts capability refresh without recreating the transport client", async () => {
    const bridge = new DshBridge(() => undefined, () => ({
      start: () => undefined,
      initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
      request: async () => ({}),
      subscribeSessionTree: () => ({ next: async () => new Promise<never>(() => undefined), close: () => undefined }),
      close: async () => undefined,
    }), "root");
    await expect(bridge.command({ method: "interaction/capability-refresh", params: { interaction_id: "i-1", generation: 8 } })).resolves.toEqual({ interaction_id: "i-1", generation: 8 });
    await bridge.dispose();
  });

  test("forwards the runtime ready notification as the single ready event", () => {
    expect(mapHarnessNotification({
      method: "session.ready",
      params: { sessionId: "s", presetId: "project-creator" },
    })).toEqual([{
      event: "session.ready",
      data: { session_id: "s", preset_id: "project-creator" },
    }]);
    expect(mapHarnessNotification({
      method: "session.ready",
      params: { sessionId: "child", presetId: "project-creator" },
    }, "root")).toEqual([]);
    expect(mapHarnessNotification({
      method: "session.status",
      params: { sessionId: "child", status: "running" },
    }, "root")).toEqual([]);
  });

  test("parses supported commands and rejects unknown methods", () => {
    expect(parseInteractiveCommand(JSON.stringify({
      id: 3,
      method: "session/answer",
      params: { sessionId: "s", callId: "c", answer: { answers: [] } },
    }))).toEqual({
      id: 3,
      method: "session/answer",
      params: { sessionId: "s", callId: "c", answer: { answers: [] } },
    });
    expect(() => parseInteractiveCommand('{"method":"unknown","params":{}}')).toThrow(
      "unsupported DSH bridge method",
    );
  });

  test("maps assistant text and AskUserQuestion events", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "assistant/message",
          data: { message: { content: [{ type: "text", text: "hello" }, { type: "text", text: " world" }] } },
        },
      },
    })).toEqual([{ event: "assistant.message", data: { session_id: "s", text: "hello world" } }]);

    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/call",
          data: {
            callId: "call-1",
            name: "ask_user_question",
            arguments: JSON.stringify({ questions: [{ id: "q", question: "What?", multiSelect: true }] }),
          },
        },
      },
    })).toEqual([{ event: "question.opened", data: { session_id: "s", call_id: "call-1", questions: [{ id: "q", question: "What?", multiSelect: true }] } }]);
  });

  test("preserves structured recommendation metadata on question options", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/call",
          data: {
            callId: "call-recommendation",
            name: "ask_user_question",
            arguments: JSON.stringify({ questions: [{
              id: "q", question: "选择主平台",
              options: [{ label: "公众号", description: "适合深度内容", recommended: true }],
            }] }),
          },
        },
      },
    })).toEqual([{ event: "question.opened", data: {
      session_id: "s", call_id: "call-recommendation",
      questions: [{ id: "q", question: "选择主平台", options: [{ label: "公众号", description: "适合深度内容", recommended: true }] }],
    } }]);
  });

  test("maps non-question tool calls into safe agent activity events", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/call",
          data: { name: "mcp__anban__get_project_profile" },
        },
      },
    })).toEqual([{ event: "agent.activity", data: { session_id: "s", name: "get_project_profile" } }]);
  });

  test("normalizes the DSH creator MCP namespace", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/call",
          data: { name: "mcp__creator__list_projects", callId: "call-projects" },
        },
      },
    })).toEqual([{ event: "agent.activity", data: { session_id: "s", call_id: "call-projects", name: "list_projects" } }]);
  });

  test("maps tool results so the Studio can distinguish success from failure", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/result",
          data: {
            turn: 2,
            step: 3,
            message: {
              toolCallId: "call-stage",
              content: [{ type: "text", text: "stage project creator draft: interactive session not found" }],
              isError: true,
            },
            error: { code: "SESSION_NOT_FOUND" },
          },
        },
      },
    })).toEqual([{
      event: "tool.result",
      data: {
        session_id: "s",
        turn: 2,
        call_id: "call-stage",
        name: "",
        ok: false,
        message: "stage project creator draft: interactive session not found",
        code: "SESSION_NOT_FOUND",
      },
    }]);
  });

  test("maps terminal turn errors into a safe session diagnostic", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "root",
        event: {
          type: "turn/end",
          data: {
            turn: 3,
            reason: {
              kind: "error",
              error: {
                code: "AUTH_FAILED",
                message: "provider token=provider-secret",
                status: 401,
                requestId: "req-123",
              },
            },
          },
        },
      },
    }, "root")).toEqual([{
      event: "session.error",
      data: {
        session_id: "root",
        status: "runtime_failed",
        code: "AUTH_FAILED",
        message: "provider token=provider-secret",
        provider_status: 401,
        provider_request_id: "req-123",
        turn: 3,
      },
    }]);
  });

  test("forwards successful terminal turns so the server can release the command lease", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "root",
        event: { type: "turn/start", data: { turn: 4 } },
      },
    }, "root")).toEqual([{
      event: "turn.start",
      data: { session_id: "root", turn: 4 },
    }]);
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "root",
        event: { type: "turn/end", data: { turn: 4, reason: { kind: "completed" } } },
      },
    }, "root")).toEqual([{
      event: "turn.end",
      data: { session_id: "root", turn: 4, reason: { kind: "completed" } },
    }]);
  });

  test("rejects AskUserQuestion notifications without a call id", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "s",
        event: {
          type: "tool/call",
          data: { name: "ask_user_question", arguments: JSON.stringify({ questions: [{ id: "q", question: "What?" }] }) },
        },
      },
    })).toEqual([{ event: "session.error", data: { session_id: "s", message: "AskUserQuestion call id is missing" } }]);
  });

  test("turns child Agent questions into an error instead of exposing them", () => {
    expect(mapHarnessNotification({
      method: "session.event",
      params: {
        sessionId: "child",
        event: {
          type: "tool/call",
          data: {
            callId: "child-call",
            name: "ask_user_question",
            arguments: JSON.stringify({ questions: [{ id: "q", question: "Hidden?" }] }),
          },
        },
      },
    }, "root")).toEqual([{
      event: "session.error",
      data: { session_id: "child", message: "AskUserQuestion from a child Agent is not allowed" },
    }]);
  });

  test("fails closed on session and preset changes", async () => {
    const client = {
      start: () => undefined,
      initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
      request: async () => ({}),
      subscribeSessionTree: () => ({ next: async () => { throw new Error("closed"); }, close: () => undefined }),
      close: async () => undefined,
    };
    const bridge = new DshBridge(() => undefined, () => client, "root");
    await expect(bridge.command({ method: "session/prompt", params: { sessionId: "other" } }))
      .rejects.toThrow("must target the configured interactive session");
    await expect(bridge.command({ method: "session/start", params: { sessionId: "root", presetId: "profile-analysis" } }))
      .rejects.toThrow("unsupported interactive DSH preset");
    await expect(bridge.command({ method: "session/start", params: { sessionId: "root", presetId: "creation-planner" } }))
      .rejects.toThrow("does not match configured preset project-creator");
    await bridge.dispose();
  });

  test("accepts only Server-configured Assistant presets for session/start", async () => {
    for (const presetId of ["project-creator", "creation-planner", "postmortem-review"] as const) {
      const calls: Array<{ method: string; params?: object }> = [];
      const client = {
        start: () => undefined,
        initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
        request: async (method: string, params?: object) => { calls.push({ method, params }); return {}; },
        subscribeSessionTree: () => ({ next: async () => new Promise<never>(() => undefined), close: () => undefined }),
        close: async () => undefined,
      };
      const bridge = new DshBridge(() => undefined, () => client, "root", undefined, presetId);
      await bridge.command({ method: "session/start", params: { sessionId: "root", presetId } });
      expect(calls).toEqual([{ method: "session/start", params: { sessionId: "root", presetId } }]);
      await bridge.dispose();
    }
  });

  test("routes three workspace sessions through one initialized DSH client", async () => {
    process.env.ANBAN_DSH_BRIDGE_WORKSPACE_ID = "workspace-1";
    process.env.ANBAN_DSH_BRIDGE_PRESETS = "project-creator,creation-planner,postmortem-review";
    const calls: Array<{ method: string; params?: object }> = [];
    let subscriptions = 0;
    const client = {
      start: () => undefined,
      initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
      request: async (method: string, params?: object) => { calls.push({ method, params }); return {}; },
      subscribeSessionTree: () => { subscriptions += 1; return { next: async () => new Promise<never>(() => undefined), close: () => undefined }; },
      close: async () => undefined,
    };
    const bridge = new DshBridge(() => undefined, () => client, "workspace-1");
    await bridge.command({ method: "session/start", params: { sessionId: "profile-session", presetId: "project-creator" } });
    await bridge.command({ method: "session/start", params: { sessionId: "creation-session", presetId: "creation-planner" } });
    await bridge.command({ method: "session/start", params: { sessionId: "review-session", presetId: "postmortem-review" } });
    await bridge.command({ method: "session/prompt", params: { sessionId: "creation-session", contentBlocks: [] } });
    expect(calls.map((call) => call.params && (call.params as { sessionId?: string }).sessionId)).toEqual([
      "profile-session", "creation-session", "review-session", "creation-session",
    ]);
    expect(subscriptions).toBe(3);
    await expect(bridge.command({ method: "session/prompt", params: { sessionId: "unknown-session" } })).rejects.toThrow("started workspace session");
    await bridge.dispose();
    delete process.env.ANBAN_DSH_BRIDGE_WORKSPACE_ID;
    delete process.env.ANBAN_DSH_BRIDGE_PRESETS;
  });

  test("rejects an unknown Server-injected preset before initializing DSH", () => {
    expect(() => new DshBridge(() => undefined, () => {
      throw new Error("client must not be created");
    }, "root", undefined, "unknown-preset")).toThrow("unsupported interactive DSH preset");
    expect(() => validateInteractivePresetId("unknown-preset")).toThrow("unsupported interactive DSH preset");
  });

  test("announces runtime liveness after initialization", async () => {
    const events: Array<{ event: string; data?: unknown }> = [];
    const client = {
      start: () => undefined,
      initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
      request: async () => ({}),
      subscribeSessionTree: () => ({ next: async () => new Promise<never>(() => undefined), close: () => undefined }),
      close: async () => undefined,
    };
    const bridge = new DshBridge((event) => events.push(event), () => client, "root");

    await bridge.command({ method: "session/start", params: { sessionId: "root", presetId: "project-creator" } });

    expect(events.some(({ event }) => event === "session.heartbeat")).toBe(true);
    await bridge.dispose();
  });

  test("serializes answer and prompt requests", async () => {
    let releaseAnswer: (() => void) | undefined;
    const calls: string[] = [];
    const client = {
      start: () => undefined,
      initialize: async () => ({ capabilities: { sessionStart: true, sessionAnswer: true } }),
      request: async (method: string) => {
        calls.push(method);
        if (method === "session/answer") {
          await new Promise<void>((resolve) => { releaseAnswer = resolve; });
        }
        return { accepted: true };
      },
      subscribeSessionTree: () => ({ next: async () => { throw new Error("closed"); }, close: () => undefined }),
      close: async () => undefined,
    };
    const bridge = new DshBridge(() => undefined, () => client, "root");
    const answer = bridge.command({ method: "session/answer", params: { sessionId: "root" } });
    const prompt = bridge.command({ method: "session/prompt", params: { sessionId: "root" } });
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    expect(calls).toEqual(["session/answer"]);
    releaseAnswer?.();
    await expect(answer).resolves.toEqual({ accepted: true });
    await expect(prompt).resolves.toEqual({ accepted: true });
    expect(calls).toEqual(["session/answer", "session/prompt"]);
    await bridge.dispose();
  });

  test("never returns the runtime token in error payloads", () => {
    expect(redactInteractiveSecrets("token=secret-value", "secret-value")).toBe("token=[redacted]");
  });

  test("redacts provider credentials from bridge diagnostics", () => {
    expect(redactInteractiveSecrets("provider=provider-secret", "runtime-secret", ["provider-secret"]))
      .toBe("provider=[redacted]");
  });

  test("resolves an explicit DSH patch independently of the process cwd", () => {
    const dir = `/tmp/anban-dsh-bridge-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    mkdirSync(dir, { recursive: true });
    const patch = join(dir, "project-creator.patch.yml");
    writeFileSync(patch, "- insert: []\n");
    expect(resolveInteractivePatchPath(patch, "/tmp")).toBe(patch);
    expect(() => resolveInteractivePatchPath(patch, "/tmp", "creation-planner")).toThrow("does not match preset creation-planner");
    expect(() => resolveInteractivePatchPath("missing.patch.yml", dir)).toThrow("does not exist");
  });

  test("maps each allowlisted preset to a matching patch filename", () => {
    const dir = `/tmp/anban-dsh-preset-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    mkdirSync(dir, { recursive: true });
    for (const presetId of ["project-creator", "creation-planner", "postmortem-review"] as const) {
      const patch = join(dir, `${presetId}.patch.yml`);
      writeFileSync(patch, "- insert: []\n");
      expect(resolveInteractivePatchPath(patch, "/tmp", presetId)).toBe(patch);
      expect(validateInteractivePresetId(presetId)).toBe(presetId);
    }
  });

  test("requires a configured DSH patch", () => {
    expect(() => resolveInteractivePatchPath(undefined, "/tmp")).toThrow("ANBAN_DSH_BRIDGE_PATCH is required");
  });

  test("fails closed when the runtime does not advertise interactive methods", () => {
    expect(() => assertInteractiveRuntimeCapabilities({ serverInfo: { name: "dsh", version: "old" } }))
      .toThrow("interactive runtime does not support session/start");
    expect(() => assertInteractiveRuntimeCapabilities({ capabilities: { sessionStart: false, sessionAnswer: true } }))
      .toThrow("interactive runtime does not support session/start");
    expect(() => assertInteractiveRuntimeCapabilities({ capabilities: { sessionStart: true, sessionAnswer: false } }))
      .toThrow("interactive runtime does not support session/answer");
    expect(() => assertInteractiveRuntimeCapabilities({ capabilities: { sessionStart: true, sessionAnswer: true } }))
      .not.toThrow();
  });

  test("parses server-owned route arguments and writes isolated DSH settings", () => {
    const route = parseInteractiveRouteArgs([
      "--provider", "deepseek-official", "--model", "deepseek-v4-flash",
      "--route-api", "deepseek-messages", "--base-url", "https://api.deepseek.com/anthropic",
    ]);
    const home = `/tmp/anban-dsh-route-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    const path = writeDshRouteSettings(home, route);
    const text = readFileSync(path, "utf8");
    expect(text).toContain('apiKeyEnv: "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY"');
    expect(text).toContain('baseURL: "https://api.deepseek.com/anthropic"');
    expect(text).toContain("retryPolicy:");
    expect(text).toContain("- MALFORMED_RESPONSE");
    expect(text).toContain('id: "deepseek-v4-flash"');
    expect(() => parseInteractiveRouteArgs([
      "--provider", "deepseek-official", "--model", "deepseek-v4-flash",
      "--route-api", "deepseek-messages", "--base-url", "https://api.deepseek.com/anthropic",
      "--api-key-env", "DEEPSEEK_API_KEY",
    ])).toThrow("unsupported DSH bridge argument");
  });

  test("writes anthropic routes through the official llm-pi-ai fields", () => {
    const route = {
      provider: "anthropic",
      model: "deepseek-flash",
      api: "anthropic-messages",
      baseURL: "https://api.deepseek.com/anthropic",
    };
    const home = `/tmp/anban-dsh-anthropic-route-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    const text = readFileSync(writeDshRouteSettings(home, route), "utf8");
    expect(text).toContain("llm-pi-ai:");
    expect(text).toContain("    anthropic:");
    expect(text).toContain('apiKeyEnv: "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY"');
    expect(text).toContain('api: "anthropic-messages"');
    expect(text).toContain('baseURL: "https://api.deepseek.com/anthropic"');
    expect(text).toContain('id: "deepseek-flash"');
  });

  test("retries only the transient provider-registration handshake failure", async () => {
    let attempts = 0;
    const client = {
      start: () => undefined,
      initialize: async () => {
        attempts += 1;
        if (attempts < 2) throw new Error('no adapter registered for provider "anthropic"');
        return { capabilities: { sessionStart: true, sessionAnswer: true } };
      },
      request: async () => ({}),
      subscribeSessionTree: () => ({ next: async () => { throw new Error("closed"); }, close: () => undefined }),
      close: async () => undefined,
    };
    const bridge = new DshBridge(() => undefined, () => client, "root", {
      provider: "anthropic",
      model: "deepseek-flash",
      api: "anthropic-messages",
      baseURL: "https://api.deepseek.com/anthropic",
    });
    await bridge.command({ method: "session/prompt", params: { sessionId: "root" } });
    expect(attempts).toBe(2);
    await bridge.dispose();
  });
});
