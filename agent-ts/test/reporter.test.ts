import { describe, expect, test } from "bun:test";

import { Reporter, postJSONWithRetry } from "../src/reporter.js";

describe("postJSONWithRetry", () => {
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
});
