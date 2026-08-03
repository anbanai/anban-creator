import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
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
    await writeFile(join(root, "output", "nested", "b.txt"), "two");
    await writeFile(join(root, "output", "a.md"), "one");
    await symlink("a.md", join(root, "output", "link.md"));
    expect((await scanWorkspaceArtifacts(root)).map((artifact) => artifact.relativePath)).toEqual(["output/a.md", "output/nested/b.txt"]);
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
