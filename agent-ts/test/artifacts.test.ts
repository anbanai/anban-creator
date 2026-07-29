import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { scanWorkspaceArtifacts } from "../src/artifacts.js";

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
});
