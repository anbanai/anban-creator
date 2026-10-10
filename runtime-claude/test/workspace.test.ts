import { afterEach, describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { chmodSync } from "node:fs";
import { chmod, lstat, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { materializeBootstrapFiles, prepareWorkspace } from "../src/workspace.js";

const roots: string[] = [];
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

describe("materializeBootstrapFiles", () => {

  test.each(["04-article-final.md", "images.json", "cover-quality.json"])("atomically restores publication recovery %s after hash verification", async (filename) => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await mkdir(join(workspace, "output"));
    await writeFile(join(workspace, "output", filename), "stale artifact");
    const content = "verified source article";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => new Response(content, { status: 200 });
    try {
      await materializeBootstrapFiles(workspace, [{
        path: `output/${filename}`, download_url: "https://bootstrap.example/source",
        expected_size: Buffer.byteLength(content), max_bytes: 1024, mode: 0o644, replace_existing: true,
        content_sha256: createHash("sha256").update(content).digest("hex"),
      }]);
      expect(await readFile(join(workspace, "output", filename), "utf8")).toBe(content);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("rejects a downloaded bootstrap file whose SHA-256 does not match before materializing it", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => new Response("trusted source artifact", { status: 200 });
    try {
      await expect(materializeBootstrapFiles(workspace, [{
        path: "output/04-article-final.md", download_url: "https://bootstrap.example/source",
        expected_size: 23, max_bytes: 1024, mode: 0o644,
        content_sha256: "0000000000000000000000000000000000000000000000000000000000000000",
      }] as any)).rejects.toThrow("SHA-256 mismatch");
      await expect(lstat(join(workspace, "output", "04-article-final.md"))).rejects.toMatchObject({ code: "ENOENT" });
      const content = "trusted source artifact";
      await materializeBootstrapFiles(workspace, [{
        path: "output/04-article-final.md", download_url: "https://bootstrap.example/source",
        expected_size: Buffer.byteLength(content), max_bytes: 1024, mode: 0o644,
        content_sha256: createHash("sha256").update(content).digest("hex"),
      }]);
      expect(await readFile(join(workspace, "output", "04-article-final.md"), "utf8")).toBe(content);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("atomically writes an inline bootstrap file with its declared mode", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-workspace-"));
    roots.push(root);
    await materializeBootstrapFiles(root, [{ path: "inputs/request.json", text: '{"ok":true}', mode: 0o600 }]);
    expect(await readFile(join(root, "inputs/request.json"), "utf8")).toBe('{"ok":true}');
    expect((await lstat(join(root, "inputs/request.json"))).mode & 0o777).toBe(0o600);
  });

  test("refuses to traverse a symlinked bootstrap parent", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-ts-workspace-"));
    roots.push(root);
    await symlink(tmpdir(), join(root, "inputs"));
    await expect(materializeBootstrapFiles(root, [{ path: "inputs/request.json", text: "unsafe", mode: 0o600 }])).rejects.toThrow("must be a real directory");
  });

  test("does not commit an earlier file when a later bootstrap target conflicts", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await writeFile(join(workspace, "existing.txt"), "old");
    await expect(materializeBootstrapFiles(workspace, [
      { path: "new.txt", text: "new", mode: 0o644 },
      { path: "existing.txt", text: "different", mode: 0o644 },
    ])).rejects.toThrow("conflicts");
    await expect(lstat(join(workspace, "new.txt"))).rejects.toMatchObject({ code: "ENOENT" });
  });

  test("replaces a differing managed settings file", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await mkdir(join(workspace, ".anban-creator"));
    await writeFile(join(workspace, ".anban-creator", "settings.json"), "old", { mode: 0o600 });

    await materializeBootstrapFiles(workspace, [{
      path: ".anban-creator/settings.json", text: "new", mode: 0o600,
      replace_existing: true,
    }]);

    expect(await readFile(join(workspace, ".anban-creator", "settings.json"), "utf8")).toBe("new");
  });

  test("does not replace managed settings when another target conflicts", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await mkdir(join(workspace, ".anban-creator"));
    await writeFile(join(workspace, ".anban-creator", "settings.json"), "old", { mode: 0o600 });
    await writeFile(join(workspace, "immutable.txt"), "old");

    await expect(materializeBootstrapFiles(workspace, [
      { path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true },
      { path: "immutable.txt", text: "different", mode: 0o644 },
    ])).rejects.toThrow("conflicts");

    expect(await readFile(join(workspace, ".anban-creator", "settings.json"), "utf8")).toBe("old");
  });

  test("refuses to replace a symlink target", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await mkdir(join(workspace, ".anban-creator"));
    await symlink(join(workspace, "outside.json"), join(workspace, ".anban-creator", "settings.json"));

    await expect(materializeBootstrapFiles(workspace, [{
      path: ".anban-creator/settings.json", text: "new", mode: 0o600,
      replace_existing: true,
    }])).rejects.toThrow("not a regular file");
  });

  test("rolls back a replacement when a later commit is aborted", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    await mkdir(join(workspace, ".anban-creator"));
    await writeFile(join(workspace, ".anban-creator", "settings.json"), "old", { mode: 0o600 });
    let checks = 0;
    const signal = {
      throwIfAborted() {
        checks += 1;
        if (checks === 4) throw new Error("commit aborted");
      },
    } as AbortSignal;

    await expect(materializeBootstrapFiles(workspace, [
      { path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true },
      { path: "created.txt", text: "created", mode: 0o644 },
    ], signal)).rejects.toThrow("commit aborted");

    expect(await readFile(join(workspace, ".anban-creator", "settings.json"), "utf8")).toBe("old");
    await expect(lstat(join(workspace, "created.txt"))).rejects.toMatchObject({ code: "ENOENT" });
  });

  test("preserves the backup and continues rollback when restore is incomplete", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    roots.push(workspace);
    const settingsDirectory = join(workspace, ".anban-creator");
    const settingsPath = join(settingsDirectory, "settings.json");
    await mkdir(settingsDirectory);
    await writeFile(settingsPath, "old", { mode: 0o600 });
    let checks = 0;
    const signal = {
      throwIfAborted() {
        checks += 1;
        if (checks === 5) chmodSync(settingsDirectory, 0o500);
      },
    } as AbortSignal;

    try {
      await expect(materializeBootstrapFiles(workspace, [
        { path: "created.txt", text: "created", mode: 0o644 },
        { path: ".anban-creator/settings.json", text: "new", mode: 0o600, replace_existing: true },
      ], signal)).rejects.toThrow("recovery files preserved at");

      await expect(lstat(join(workspace, "created.txt"))).rejects.toMatchObject({ code: "ENOENT" });
      await expect(lstat(settingsPath)).rejects.toMatchObject({ code: "ENOENT" });
      const recoveryRoots = (await readdir(workspace)).filter((entry) => entry.startsWith(".anban-bootstrap-"));
      expect(recoveryRoots).toHaveLength(1);
      const backupDirectory = join(workspace, recoveryRoots[0]!, "backups");
      const backupFiles = await readdir(backupDirectory);
      expect(backupFiles).toHaveLength(1);
      expect(await readFile(join(backupDirectory, backupFiles[0]!), "utf8")).toBe("old");
    } finally {
      chmodSync(settingsDirectory, 0o700);
    }
  });
});

describe("prepareWorkspace", () => {
  test("makes a copied Montage template writable for the runtime user", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-workspace-"));
    const template = await mkdtemp(join(tmpdir(), "anban-montage-template-"));
    roots.push(workspace, template);
    await mkdir(join(template, "remotion-composer"));
    await writeFile(join(template, "remotion-composer", "package.json"), "{}");
    await chmod(join(template, "remotion-composer"), 0o555);
    await chmod(join(template, "remotion-composer", "package.json"), 0o444);
    const previous = process.env.ANBAN_MONTAGE_TEMPLATE_PATH;
    process.env.ANBAN_MONTAGE_TEMPLATE_PATH = template;
    try {
      await prepareWorkspace(workspace, "montage", "openmontage");
      expect((await lstat(join(workspace, "openmontage", "remotion-composer"))).mode & 0o700).toBe(0o700);
      expect((await lstat(join(workspace, "openmontage", "remotion-composer", "package.json"))).mode & 0o600).toBe(0o600);
    } finally {
      if (previous === undefined) delete process.env.ANBAN_MONTAGE_TEMPLATE_PATH;
      else process.env.ANBAN_MONTAGE_TEMPLATE_PATH = previous;
      await chmod(join(template, "remotion-composer"), 0o755);
    }
  });
});
