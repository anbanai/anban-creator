import { describe, expect, test } from "bun:test";
import {
  mapHarnessNotification,
  parseInteractiveCommand,
  redactInteractiveSecrets,
  resolveInteractivePatchPath,
  assertInteractiveRuntimeCapabilities,
  InteractiveBridge,
  parseInteractiveRouteArgs,
  writeDshRouteSettings,
} from "../src/interactive-bridge.js";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

describe("interactive bridge protocol", () => {
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
      "unsupported interactive bridge method",
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
    const bridge = new InteractiveBridge(() => undefined, () => client, "root");
    await expect(bridge.command({ method: "session/prompt", params: { sessionId: "other" } }))
      .rejects.toThrow("must target the configured interactive session");
    await expect(bridge.command({ method: "session/start", params: { sessionId: "root", presetId: "profile-analysis" } }))
      .rejects.toThrow("must use the project-creator preset");
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
    const bridge = new InteractiveBridge(() => undefined, () => client, "root");
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
    const dir = `/tmp/anban-interactive-bridge-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    mkdirSync(dir, { recursive: true });
    const patch = join(dir, "project-creator.patch.yml");
    writeFileSync(patch, "- insert: []\n");
    expect(resolveInteractivePatchPath(patch, "/tmp")).toBe(patch);
    expect(() => resolveInteractivePatchPath("missing.patch.yml", dir)).toThrow("does not exist");
  });

  test("requires a configured DSH patch", () => {
    expect(() => resolveInteractivePatchPath(undefined, "/tmp")).toThrow("ANBAN_INTERACTIVE_DSH_PATCH is required");
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
    expect(text).toContain('apiKeyEnv: "ANBAN_INTERACTIVE_PROVIDER_API_KEY"');
    expect(text).toContain('baseURL: "https://api.deepseek.com/anthropic"');
    expect(text).toContain('id: "deepseek-v4-flash"');
    expect(() => parseInteractiveRouteArgs([
      "--provider", "deepseek-official", "--model", "deepseek-v4-flash",
      "--route-api", "deepseek-messages", "--base-url", "https://api.deepseek.com/anthropic",
      "--api-key-env", "DEEPSEEK_API_KEY",
    ])).toThrow("unsupported interactive bridge argument");
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
    expect(text).toContain('apiKeyEnv: "ANBAN_INTERACTIVE_PROVIDER_API_KEY"');
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
    const bridge = new InteractiveBridge(() => undefined, () => client, "root", {
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
