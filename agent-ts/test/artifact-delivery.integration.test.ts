import { afterEach, describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { BootstrapResponse } from "../src/bootstrap.js";
import { uploadWorkspaceArtifacts } from "../src/artifacts.js";
import { Reporter } from "../src/reporter.js";

const roots: string[] = [];
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

describe("artifact delivery fault injection", () => {
  test("recovers image-review prepare 503 and a lost manifest acknowledgement without regenerating", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-artifact-integration-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    const paths = ["image-review.md", ...Array.from({ length: 15 }, (_, index) => `artifact-${index}.txt`)];
    const reviewContents = Buffer.alloc(2 * 1024 * 1024, 0x61);
    await Promise.all(paths.map((path) => writeFile(join(root, "output", path), path === "image-review.md" ? reviewContents : `contents:${path}`)));

    let reviewPrepareAttempts = 0;
    let reviewPutAttempts = 0;
    let manifestAttempts = 0;
    let durableManifest: unknown;
    const uploaded = new Map<string, Uint8Array>();
    const server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url);
        if (url.pathname === "/api/v1/agent/artifacts/prepare") {
          const body = await request.json() as { relative_path: string; sha256: string; size: number };
          if (body.relative_path === "output/image-review.md" && ++reviewPrepareAttempts === 1) return new Response("temporary", { status: 503 });
          const key = `staging/${body.relative_path.slice("output/".length)}`;
          return Response.json({ code: 0, data: { upload_required: true, key, upload_url: `${server.url}upload/${encodeURIComponent(key)}`, method: "PUT", headers: { "X-Oss-Meta-Sha256": body.sha256 }, expires_at: new Date(Date.now() + 60_000).toISOString(), max_size: body.size } });
        }
        if (url.pathname.startsWith("/upload/")) {
          const key = decodeURIComponent(url.pathname.slice("/upload/".length));
          if (key === "staging/image-review.md" && ++reviewPutAttempts === 1) return new Response("temporary", { status: 503 });
          const bytes = new Uint8Array(await request.arrayBuffer());
          expect(createHash("sha256").update(bytes).digest("hex")).toBe(request.headers.get("X-Oss-Meta-Sha256"));
          uploaded.set(key, bytes);
          return new Response(null, { status: 200 });
        }
        if (url.pathname === "/api/v1/agent/artifacts/manifest") {
          const body = await request.json();
          manifestAttempts += 1;
          durableManifest ??= body;
          expect(body).toEqual(durableManifest);
          if (manifestAttempts === 1) return new Response("ack lost", { status: 503 });
          return Response.json({ code: 0 });
        }
        if (url.pathname === "/api/v1/agent/progress") return Response.json({ code: 0 });
        return new Response("not found", { status: 404 });
      },
    });
    try {
      const reporter = new Reporter({ serverURL: String(server.url).replace(/\/$/, ""), executionID: "execution-1" }, "token", "task-1");
      const summary = await uploadWorkspaceArtifacts(root, { artifact_transport: { mode: "direct" } } as BootstrapResponse, reporter);
      expect(summary).toEqual({ uploaded: 16, failures: [] });
      expect(reviewPrepareAttempts).toBe(2);
      expect(reviewPutAttempts).toBe(2);
      expect(uploaded.get("staging/image-review.md")?.byteLength).toBe(reviewContents.byteLength);
      expect(createHash("sha256").update(uploaded.get("staging/image-review.md")!).digest("hex")).toBe(createHash("sha256").update(reviewContents).digest("hex"));
      expect(uploaded.size).toBe(16);
      expect(manifestAttempts).toBe(2);
      expect((durableManifest as { files: Array<{ relative_path: string }> }).files.map((file) => file.relative_path)).toContain("output/image-review.md");
    } finally {
      server.stop(true);
    }
  });

  test("reopens a large stream body after an early server rejection", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-artifact-stream-integration-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    const contents = Buffer.alloc(2 * 1024 * 1024, 0x62);
    await writeFile(join(root, "output", "large.bin"), contents);
    let streamAttempts = 0;
    let receivedHash = "";
    const server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url);
        if (url.pathname === "/api/v1/agent/artifacts/content") {
          streamAttempts += 1;
          if (streamAttempts === 1) return new Response("temporary", { status: 503 });
          const bytes = new Uint8Array(await request.arrayBuffer());
          receivedHash = createHash("sha256").update(bytes).digest("hex");
          return Response.json({ code: 0, data: { object_key: "final/large.bin", content_type: "application/octet-stream", size: bytes.byteLength, sha256: receivedHash } });
        }
        if (url.pathname === "/api/v1/agent/artifacts/manifest" || url.pathname === "/api/v1/agent/progress") return Response.json({ code: 0 });
        return new Response("not found", { status: 404 });
      },
    });
    try {
      const reporter = new Reporter({ serverURL: String(server.url).replace(/\/$/, ""), executionID: "execution-1" }, "token", "task-1");
      const summary = await uploadWorkspaceArtifacts(root, { artifact_transport: { mode: "stream" } } as BootstrapResponse, reporter);
      expect(summary).toEqual({ uploaded: 1, failures: [] });
      expect(streamAttempts).toBe(2);
      expect(receivedHash).toBe(createHash("sha256").update(contents).digest("hex"));
    } finally {
      server.stop(true);
    }
  });

  test("skips a long Retry-After file and still uploads later files and the manifest", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-artifact-retry-after-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(join(root, "output", "a-optional.txt"), "optional");
    await writeFile(join(root, "output", "b-required.txt"), "required");
    const uploaded: string[] = [];
    let manifestPaths: string[] = [];
    const server = Bun.serve({
      port: 0,
      async fetch(request) {
        const url = new URL(request.url);
        if (url.pathname === "/api/v1/agent/artifacts/prepare") {
          const body = await request.json() as { relative_path: string; sha256: string };
          return Response.json({ code: 0, data: { upload_required: true, key: body.relative_path, upload_url: `${server.url}upload/${encodeURIComponent(body.relative_path)}`, method: "PUT", headers: { "X-Oss-Meta-Sha256": body.sha256 }, expires_at: new Date(Date.now() + 60_000).toISOString(), max_size: 1024 } });
        }
        if (url.pathname.includes("a-optional")) return new Response("later", { status: 429, headers: { "Retry-After": "60" } });
        if (url.pathname.includes("b-required")) {
          await request.arrayBuffer();
          uploaded.push("output/b-required.txt");
          return new Response(null, { status: 200 });
        }
        if (url.pathname === "/api/v1/agent/artifacts/manifest") {
          const body = await request.json() as { files: Array<{ relative_path: string }> };
          manifestPaths = body.files.map((file) => file.relative_path);
          return Response.json({ code: 0 });
        }
        if (url.pathname === "/api/v1/agent/progress") return Response.json({ code: 0 });
        return new Response("not found", { status: 404 });
      },
    });
    try {
      const reporter = new Reporter({ serverURL: String(server.url).replace(/\/$/, ""), executionID: "execution-1" }, "token", "task-1");
      const deadlineAt = Date.now() + 1_000;
      const summary = await uploadWorkspaceArtifacts(root, { artifact_transport: { mode: "direct" } } as BootstrapResponse, reporter, undefined, deadlineAt);
      expect(summary).toEqual({ uploaded: 1, failures: [{ path: "output/a-optional.txt", operation: "put", code: "deadline_exceeded", attempts: 1, retryable: false, http_status: 429 }] });
      expect(uploaded).toEqual(["output/b-required.txt"]);
      expect(manifestPaths).toEqual(["output/b-required.txt"]);
      expect(Date.now()).toBeLessThan(deadlineAt);
    } finally {
      server.stop(true);
    }
  });
});
