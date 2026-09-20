import { describe, expect, test } from "bun:test";

import { TypedTransportError, retryRequest, transportErrorFromResponse } from "../src/transport.js";

const immediate = async () => {};
const dependencies = { now: () => 1_000, random: () => 0, sleep: immediate };

describe("retryRequest", () => {
  test("retries transient HTTP and network failures up to four attempts", async () => {
    for (const error of [
      () => transportErrorFromResponse("prepare", new Response("secret", { status: 503 })),
      () => Object.assign(new TypeError("fetch failed"), { cause: { code: "ECONNRESET" } }),
    ]) {
      let attempts = 0;
      const value = await retryRequest("prepare", async () => {
        attempts += 1;
        if (attempts === 1) throw error();
        return "ok";
      }, { signal: new AbortController().signal, maxAttempts: 4 }, dependencies);
      expect(value).toBe("ok");
      expect(attempts).toBe(2);
    }
  });

  test("does not retry deterministic client responses", async () => {
    for (const status of [400, 401, 403, 409]) {
      let attempts = 0;
      await expect(retryRequest("progress", async () => {
        attempts += 1;
        throw transportErrorFromResponse("progress", new Response("signed-url-secret", { status }));
      }, { signal: new AbortController().signal, maxAttempts: 4 }, dependencies)).rejects.toBeInstanceOf(TypedTransportError);
      expect(attempts).toBe(1);
    }
  });

  test("returns a structured exhausted failure without response secrets", async () => {
    let attempts = 0;
    const error = await retryRequest("manifest", async () => {
      attempts += 1;
      throw transportErrorFromResponse("manifest", new Response("FAKE_SECRET", {
        status: 503,
        headers: { "X-Request-ID": "request-1" },
      }));
    }, { signal: new AbortController().signal, maxAttempts: 4 }, dependencies).catch((caught) => caught as TypedTransportError);

    expect(attempts).toBe(4);
    expect(error.failure).toEqual({ operation: "manifest", code: "service_unavailable", http_status: 503, attempts: 4, retryable: true, request_id: "request-1" });
    expect(JSON.stringify(error)).not.toContain("FAKE_SECRET");
  });

  test("stops immediately when the parent signal is cancelled", async () => {
    const controller = new AbortController();
    controller.abort(new Error("shutdown"));
    let attempts = 0;
    const error = await retryRequest("put", async () => { attempts += 1; }, { signal: controller.signal, maxAttempts: 4 }, dependencies).catch((caught) => caught as TypedTransportError);
    expect(attempts).toBe(0);
    expect(error.failure).toMatchObject({ code: "cancelled", attempts: 0, retryable: false });
  });

  test("does not retry when Retry-After exceeds the shared deadline", async () => {
    let attempts = 0;
    const error = await retryRequest("prepare", async () => {
      attempts += 1;
      throw transportErrorFromResponse("prepare", new Response("", { status: 503, headers: { "Retry-After": "10" } }));
    }, { signal: new AbortController().signal, maxAttempts: 4, deadlineAt: 5_000 }, dependencies).catch((caught) => caught as TypedTransportError);
    expect(attempts).toBe(1);
    expect(error.failure.code).toBe("deadline_exceeded");
  });

  test("classifies a parent phase deadline during an active request", async () => {
    const controller = new AbortController();
    const pending = retryRequest("stream", async (signal) => new Promise<void>((_resolve, reject) => {
      signal.addEventListener("abort", () => reject(signal.reason), { once: true });
    }), { signal: controller.signal, maxAttempts: 4 });
    controller.abort(new Error("phase deadline exceeded"));
    const error = await pending.catch((caught) => caught as TypedTransportError);
    expect(error.failure).toMatchObject({ operation: "stream", code: "deadline_exceeded", attempts: 1, retryable: false });
  });
});
