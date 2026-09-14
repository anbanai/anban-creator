import { afterEach, describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rename, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { BootstrapResponse } from "../src/bootstrap.js";
import { scanWorkspaceArtifacts, uploadWorkspaceArtifacts } from "../src/artifacts.js";
import { Reporter } from "../src/reporter.js";

const roots: string[] = [];
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

describe("scanWorkspaceArtifacts", () => {
  test("collects stable regular files below output in lexical order", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output", "nested"), { recursive: true });
    await mkdir(join(root, ".claude", "memory"), { recursive: true });
    await writeFile(join(root, "output", "nested", "b.txt"), "two");
    await writeFile(join(root, "output", "a.md"), "one");
    await writeFile(join(root, ".claude", "memory", "MEMORY.md"), "must stay on NAS");
    await symlink("a.md", join(root, "output", "link.md"));
    expect((await scanWorkspaceArtifacts(root)).map((artifact) => artifact.relativePath)).toEqual(["output/a.md", "output/nested/b.txt"]);
  });

  test("preserves the legacy delivery filter for runtime and build files", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output", "nested"), { recursive: true });
    await mkdir(join(root, "output", ".custom"), { recursive: true });
    for (const directory of [".anban-creator", ".anban-runtime-home", ".claude", ".git", "node_modules", "dist", "build", ".cache", ".vite"]) {
      await mkdir(join(root, "output", directory), { recursive: true });
      await writeFile(join(root, "output", directory, "ignored.txt"), "ignored");
    }
    for (const file of ["package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "tsconfig.json", "vite.config.ts", "vite.config.js", "eslint.config.js", "eslint.config.mjs", ".hidden"]) {
      await writeFile(join(root, "output", file), "ignored");
    }
    await writeFile(join(root, "output", "nested", "content.md"), "kept");
    await writeFile(join(root, "output", ".custom", "kept.txt"), "kept");

    expect((await scanWorkspaceArtifacts(root)).map((artifact) => artifact.relativePath)).toEqual([
      "output/.custom/kept.txt",
      "output/nested/content.md",
    ]);
  });

  test("detects image bytes and preserves the stream response content type", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(join(root, "output", "preview.bin"), Buffer.from("GIF89a", "ascii"));
    let manifested: Array<{ content_type: string }> = [];
    const reporter = {
      progress: async () => {},
      prepareArtifactUpload: async () => { throw new Error("unexpected direct upload"); },
      streamArtifactContent: async (metadata: { content_type: string; size: number; sha256: string }) => {
        expect(metadata.content_type).toBe("image/gif");
        return { object_key: "artifacts/preview.bin", content_type: "image/gif; verified=true", size: metadata.size, sha256: metadata.sha256 };
      },
      reportArtifactManifest: async (files: Array<{ content_type: string }>) => { manifested = files; },
    };
    const bootstrap = { artifact_transport: { mode: "stream" } } as BootstrapResponse;

    expect(await uploadWorkspaceArtifacts(root, bootstrap, reporter)).toEqual({ uploaded: 1, failures: [] });
    expect(manifested).toEqual([{ content_type: "image/gif; verified=true", object_key: "artifacts/preview.bin", relative_path: "output/preview.bin", sha256: expect.any(String), size: 6 }]);
  });

  test("uploads from the hashed file handle when the visible path is replaced", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    const artifactPath = join(root, "output", "content.md");
    const replacementPath = join(root, "replacement.md");
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(artifactPath, "first");
    await writeFile(replacementPath, "second");
    const uploaded: string[] = [];
    let manifested: Array<{ sha256: string }> = [];
    const reporter = {
      progress: async () => {},
      prepareArtifactUpload: async () => { throw new Error("unexpected direct upload"); },
      streamArtifactContent: async (metadata: { content_type: string; size: number; sha256: string }, body: ReadableStream<Uint8Array>) => {
        if (uploaded.length === 0) await rename(replacementPath, artifactPath);
        const contents = await new Response(body).text();
        uploaded.push(contents);
        expect(createHash("sha256").update(contents).digest("hex")).toBe(metadata.sha256);
        return { object_key: `artifacts/content-${uploaded.length}.md`, content_type: metadata.content_type, size: metadata.size, sha256: metadata.sha256 };
      },
      reportArtifactManifest: async (files: Array<{ sha256: string }>) => { manifested = files; },
    };
    const bootstrap = { artifact_transport: { mode: "stream" } } as BootstrapResponse;

    expect(await uploadWorkspaceArtifacts(root, bootstrap, reporter)).toEqual({ uploaded: 1, failures: [] });
    expect(uploaded).toEqual(["first", "second"]);
    expect(manifested[0]?.sha256).toBe(createHash("sha256").update("second").digest("hex"));
  });

  test("submits successful files when another artifact upload fails", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(join(root, "output", "broken.md"), "broken");
    await writeFile(join(root, "output", "kept.md"), "kept");
    const progress: string[] = [];
    let manifested: Array<{ relative_path: string }> = [];
    const reporter = {
      progress: async (message: string) => { progress.push(message); },
      prepareArtifactUpload: async () => { throw new Error("unexpected direct upload"); },
      streamArtifactContent: async (metadata: { relative_path: string; content_type: string; size: number; sha256: string }) => {
        if (metadata.relative_path === "output/broken.md") throw new Error("storage unavailable");
        return { object_key: "artifacts/kept.md", content_type: metadata.content_type, size: metadata.size, sha256: metadata.sha256 };
      },
      reportArtifactManifest: async (files: Array<{ relative_path: string }>) => { manifested = files; },
    };

    expect(await uploadWorkspaceArtifacts(root, { artifact_transport: { mode: "stream" } } as BootstrapResponse, reporter)).toEqual({
      uploaded: 1,
      failures: [{ path: "output/broken.md", reason: "upload artifact output/broken.md: storage unavailable" }],
    });
    expect(manifested.map((file) => file.relative_path)).toEqual(["output/kept.md"]);
    expect(progress).toContain("artifact upload failed: output/broken.md: upload artifact output/broken.md: storage unavailable");
    expect(progress).toContain("uploaded 1 workspace artifact(s)");
    expect(progress.some((message) => message.includes("collected"))).toBe(false);
  });

  test("rejects a scan that is already cancelled", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(join(root, "output", "content.md"), "content");
    const controller = new AbortController();
    controller.abort(new Error("shutdown"));

    await expect(scanWorkspaceArtifacts(root, controller.signal)).rejects.toThrow("shutdown");
  });

  test("rejects more files than one server manifest can accept", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await Promise.all(Array.from({ length: 257 }, (_, index) =>
      writeFile(join(root, "output", `file-${index.toString().padStart(3, "0")}.md`), "content"),
    ));

    await expect(scanWorkspaceArtifacts(root)).rejects.toThrow("at most 256 files");
  });

  test("cancels a signed upload without submitting a partial manifest", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-artifacts-"));
    roots.push(root);
    await mkdir(join(root, "output"), { recursive: true });
    await writeFile(join(root, "output", "content.md"), "content");
    const controller = new AbortController();
    const originalFetch = globalThis.fetch;
    let manifested = false;
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      if (url.endsWith("/api/v1/agent/artifacts/prepare")) {
        const request = JSON.parse(String(init?.body)) as { sha256: string };
        return new Response(JSON.stringify({ code: 0, data: {
          upload_required: true,
          key: "staging/content.md",
          upload_url: "https://oss.example.test/content.md",
          method: "PUT",
          headers: { "X-Oss-Meta-Sha256": request.sha256 },
          max_size: 1024,
        } }), { status: 200 });
      }
      if (url === "https://oss.example.test/content.md") {
        expect(init?.signal).toBe(controller.signal);
        controller.abort(new Error("shutdown"));
        throw controller.signal.reason;
      }
      if (url.endsWith("/api/v1/agent/artifacts/manifest")) manifested = true;
      return new Response("{}", { status: 200 });
    };
    try {
      const reporter = new Reporter({
        serverURL: "https://creator.example.test",
        executionID: "execution-1",
        workspace: root,
        workloadTokenFile: "/token",
        allowHTTPServer: false,
      }, "execution-token", "task-1");
      const bootstrap = { artifact_transport: { mode: "direct" } } as BootstrapResponse;
      await expect(uploadWorkspaceArtifacts(root, bootstrap, reporter, controller.signal)).rejects.toThrow("shutdown");
      expect(manifested).toBe(false);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
