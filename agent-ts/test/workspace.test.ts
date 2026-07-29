import { afterEach, describe, expect, test } from "bun:test";
import { lstat, mkdtemp, readFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { materializeBootstrapFiles } from "../src/workspace.js";

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
});
