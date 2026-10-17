import { describe, expect, test } from "bun:test";
import { DeepSeekAdapter } from "@deepseek-ai/dsh-llm-deepseek";

const connection = {
  baseURL: "https://example.test/anthropic",
  defaults: { thinking: "disabled" as const },
  maxTokens: 100,
  defaultContextWindow: 1_000,
  models: [{ id: "deepseek-flash", name: "test", inputModalities: ["text" as const] }],
  streamIdleTimeoutMs: 30_000,
  maxRequestFilesBytes: 1_000_000,
  maxInlineRequestImageBytes: 1_000_000,
  maxImagesPerRequest: 10,
  imageOffloadByteQuantum: 1_000,
  inlineImageOffloadByteQuantum: 1_000,
  imageOffloadCountQuantum: 1,
  filesApiTimeoutMs: 1_000,
  filePolicy: { expiresAfterSeconds: 3_600, refreshMarginSeconds: 60, quotaCleanupBatch: 1 },
  retryPolicy: {
    mode: "normal" as const,
    maxRetries: 2,
    retryableCodes: [],
    initialDelayMs: 1,
    maxDelayMs: 1,
    jitterRatio: 0,
  },
};

function sse(events: Array<{ event: string; data: unknown }>): string {
  return events.map(({ event, data }) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join("");
}

describe("DeepSeek Messages adapter", () => {
  test("keeps truncated tool JSON recoverable for ToolRuntime", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () => new Response(sse([
      { event: "message_start", data: { type: "message_start", message: { usage: {} } } },
      {
        event: "content_block_start",
        data: {
          type: "content_block_start",
          index: 0,
          content_block: { type: "tool_use", id: "call-1", name: "ask", input: {} },
        },
      },
      {
        event: "content_block_delta",
        data: {
          type: "content_block_delta",
          index: 0,
          delta: { type: "input_json_delta", partial_json: "{\"x\":" },
        },
      },
      { event: "content_block_stop", data: { type: "content_block_stop", index: 0 } },
      {
        event: "message_delta",
        data: { type: "message_delta", delta: { stop_reason: "tool_use" }, usage: { output_tokens: 1 } },
      },
      { event: "message_stop", data: { type: "message_stop" } },
    ]), { status: 200, headers: { "content-type": "text/event-stream" } })) as typeof fetch;
    try {
      const adapter = new DeepSeekAdapter({
        options: () => connection,
        resolveAuth: async () => ({ headers: { "x-api-key": "test" } }),
        resolveUserId: () => "test-user" as never,
        prepareExtensions: async () => ({ fields: {}, accept: async () => undefined }),
      });
      const chunks = [];
      for await (const chunk of adapter.stream({
        provider: "deepseek-official",
        model: "deepseek-flash",
        messages: [{ role: "user", content: [{ type: "text", text: "hello" }] }],
      })) chunks.push(chunk);
      expect(chunks.at(-1)).toMatchObject({ type: "finish", reason: { kind: "tool-calls" } });
      expect(chunks.find((chunk) => chunk.type === "block-end")).toMatchObject({
        block: { type: "tool-call", arguments: "{\"x\":" },
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
