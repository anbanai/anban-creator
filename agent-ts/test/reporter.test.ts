import { describe, expect, test } from "bun:test";

import { Reporter, postJSONWithRetry } from "../src/reporter.js";

describe("postJSONWithRetry", () => {
  test("submits completion metadata through a session-capable MCP request", async () => {
    const requests: Array<{ body: unknown; headers: Headers }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (_input, init) => {
      requests.push({ body: JSON.parse(String(init?.body)), headers: new Headers(init?.headers) });
      if (requests.length === 1) return new Response(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }), { status: 200, headers: { "Content-Type": "application/json", "Mcp-Session-Id": "session-1" } });
      if (requests.length === 2) return new Response("", { status: 202 });
      return new Response(JSON.stringify({ jsonrpc: "2.0", id: 2, result: { content: [{ type: "text", text: "ok" }] } }), { status: 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      await reporter.submitCompletionMetadata({ tags: [] });
      expect(requests[0]?.headers.get("Accept")).toBe("application/json, text/event-stream");
      expect(requests[2]?.headers.get("Mcp-Session-Id")).toBe("session-1");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("retries a transient terminal failure three times", async () => {
    let attempts = 0;
    await postJSONWithRetry(
      async () => {
        attempts += 1;
        if (attempts < 3) throw new Error("temporary failure");
      },
      async () => {},
    );
    expect(attempts).toBe(3);
  });

  test("stops retry backoff when the completion deadline is cancelled", async () => {
    const controller = new AbortController();
    let attempts = 0;
    const pending = postJSONWithRetry(
      async () => {
        attempts += 1;
        throw new Error("temporary failure");
      },
      undefined,
      controller.signal,
    );
    await Bun.sleep(10);
    controller.abort(new Error("completion deadline exceeded"));

    await expect(pending).rejects.toThrow("completion deadline exceeded");
    expect(attempts).toBe(1);
  });

  test("reports progress with the server task and execution identity", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (input, init) => {
      requests.push({ url: String(input), body: JSON.parse(String(init?.body)) });
      return new Response("{}", { status: 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1", workspace: "/workspace", workloadTokenFile: "/token", allowHTTPServer: false }, "execution-token", "task-1");
      await reporter.progress("working");
      expect(requests).toEqual([{ url: "https://creator.example.com/api/v1/agent/progress", body: { task_id: "task-1", execution_id: "execution-1", message: "working" } }]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("reports structured stage progress with state and identity", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (input, init) => {
      requests.push({ url: String(input), body: JSON.parse(String(init?.body)) });
      return new Response("{}", { status: 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      await reporter.stageProgress({
        stage: "research",
        state: "active",
        title: "Research",
        description: "Gathering sources",
        progress_percent: 10,
      });
      expect(requests).toEqual([{
        url: "https://creator.example.com/api/v1/agent/progress",
        body: {
          task_id: "task-1",
          execution_id: "execution-1",
          stage: "research",
          state: "active",
          title: "Research",
          description: "Gathering sources",
          progress_percent: 10,
        },
      }]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("reports completion with task and execution identity", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (input, init) => {
      requests.push({ url: String(input), body: JSON.parse(String(init?.body)) });
      return new Response("{}", { status: 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      const result = { success: false, error: "agent failed", log_text: "terminal log" };
      await reporter.complete(result);
      expect(requests).toEqual([{
        url: "https://creator.example.com/api/v1/agent/complete",
        body: { task_id: "task-1", execution_id: "execution-1", result },
      }]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("retries the identical completion body after the server commits but the response is lost", async () => {
    const requests: unknown[] = [];
    let durableCompletions = 0;
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (_input, init) => {
      const body = JSON.parse(String(init?.body));
      requests.push(body);
      if (durableCompletions === 0) {
        durableCompletions += 1;
        throw new Error("response lost after commit");
      }
      return new Response("{}", { status: 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      const result = { success: false, error: "agent failed", tool_use_summary: { Write: 1 } };
      await reporter.complete(result);

      expect(durableCompletions).toBe(1);
      expect(requests).toHaveLength(2);
      expect(requests[1]).toEqual(requests[0]);
      expect(requests[0]).toEqual({ task_id: "task-1", execution_id: "execution-1", result });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("retries structured stage progress twice before succeeding", async () => {
    let attempts = 0;
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => {
      attempts += 1;
      return new Response("{}", { status: attempts < 3 ? 503 : 200 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      await reporter.stageProgress({ stage: "research", state: "active", title: "Research", progress_percent: 10 });
      expect(attempts).toBe(3);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("aborts structured progress during retry backoff", async () => {
    let attempts = 0;
    const controller = new AbortController();
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => {
      attempts += 1;
      return new Response("{}", { status: 503 });
    };
    try {
      const reporter = new Reporter({ serverURL: "https://creator.example.com", executionID: "execution-1" }, "execution-token", "task-1");
      const pending = reporter.stageProgress({ stage: "research", state: "active", title: "Research", progress_percent: 10 }, controller.signal);
      await Bun.sleep(10);
      controller.abort(new Error("progress deadline exceeded"));

      await expect(pending).rejects.toThrow("progress deadline exceeded");
      expect(attempts).toBe(1);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
