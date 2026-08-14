import { afterEach, describe, expect, test } from "bun:test";
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { materializeBootstrapFiles, prepareWorkspace } from "../src/workspace.js";

const roots: string[] = [];
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

describe("materializeBootstrapFiles", () => {
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
