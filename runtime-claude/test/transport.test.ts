import { describe, expect, test } from "bun:test";

import { TypedTransportError, describeTransportError, retryRequest, transportErrorFromResponse } from "../src/transport.js";

const immediate = async () => {};
const dependencies = { now: () => 1_000, random: () => 0, sleep: immediate };

describe("describeTransportError", () => {
  test("flattens a nested fetch cause into a single log line", async () => {
    const error = await retryRequest("bootstrap", async () => {
      throw Object.assign(new TypeError("fetch failed"), {
        cause: { cause: { code: "ENOTFOUND" }, message: "getaddrinfo ENOTFOUND" },
      });
    }, { signal: new AbortController().signal, maxAttempts: 1 }, dependencies).catch((caught) => caught);

    const line = describeTransportError("bootstrap", error, "http://creator-server-svc.anbanai-prod.svc.cluster.local:8080");
    expect(line).toContain("bootstrap failed:");
    expect(line).toContain("failure=network_unavailable");
    expect(line).toContain("network_code=ENOTFOUND");
    expect(line).toContain("host=creator-server-svc.anbanai-prod.svc.cluster.local");
    expect(line).toContain("port=8080");
    expect(line).not.toContain("getaddrinfo");
  });

  test("distinguishes a refused connection from a DNS failure", async () => {
    for (const code of ["ECONNREFUSED", "EAI_AGAIN", "ENETUNREACH"]) {
      const error = Object.assign(new TypeError("fetch failed"), { cause: { code } });
      expect(describeTransportError("bootstrap", error, "https://creator.example")).toContain(`network_code=${code}`);
    }
  });

  test("defaults the port from the endpoint scheme", () => {
    const error = Object.assign(new TypeError("fetch failed"), { cause: { code: "ECONNREFUSED" } });
    expect(describeTransportError("bootstrap", error, "https://creator.example")).toContain("port=443");
    expect(describeTransportError("bootstrap", error, "http://creator.example")).toContain("port=80");
  });

  test("reports an HTTP rejection without a network cause", () => {
    const error = new Error("bootstrap returned HTTP 426");
    const line = describeTransportError("bootstrap", error, "http://creator.example:8080");
    expect(line).toContain("failure=protocol_error");
    expect(line).toContain("message=bootstrap returned HTTP 426");
  });

  test("does not leak credentials, tokens, or URL query strings", () => {
    const error = new Error(
      "request to https://oss.example/bucket/photo.png?Signature=SECRETSIG&token=abc failed with Authorization: Bearer TOKENVALUE api_key=LEAKED",
    );
    const line = describeTransportError("bootstrap", error, "http://creator.example:8080");
    expect(line).not.toContain("SECRETSIG");
    expect(line).not.toContain("TOKENVALUE");
    expect(line).not.toContain("LEAKED");
    expect(line).toContain("port=8080");
  });
});

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

  test("retains only an allowlisted code from a nested fetch cause chain", async () => {
    const error = await retryRequest("progress", async () => {
      throw Object.assign(new TypeError("fetch failed"), {
        cause: { cause: { code: "EAI_AGAIN" }, message: "temporary DNS failure" },
      });
    }, { signal: new AbortController().signal, maxAttempts: 4 }, dependencies)
      .catch((caught) => caught as TypedTransportError);

    expect(error.failure).toEqual({
      operation: "progress",
      code: "network_unavailable",
      attempts: 4,
      retryable: true,
    });
    expect(error.networkCode).toBe("EAI_AGAIN");
    expect(JSON.stringify(error.failure)).not.toContain("temporary DNS failure");
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
