import { afterEach, expect, test } from "bun:test";
import { chmod, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { prepareProjectMemory } from "../src/project-memory.js";

const roots: string[] = [];
afterEach(async () => {
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true });
});
async function workspace() {
  const root = await mkdtemp(join(tmpdir(), "anban-memory-"));
  roots.push(root);
  return root;
}
const data = { agent_flag: "anban:article", agent_memory_directory: ".claude/agent-memory", runtime_adapter: "standard" } as const;

test("refuses a missing shared memory directory instead of creating task-local memory", async () => {
  const root = await workspace();
  await mkdir(join(root, ".claude"));
  await expect(prepareProjectMemory(root, data)).rejects.toThrow("project_memory_unavailable");
  expect(await readdir(join(root, ".claude"))).toEqual([]);
});

test("checks writability without replacing existing memory or leaving probe files", async () => {
  const root = await workspace();
  const agentDir = join(root, ".claude/agent-memory/anban-article");
  await mkdir(agentDir, { recursive: true });
  await writeFile(join(agentDir, "MEMORY.md"), "remember this");
  await prepareProjectMemory(root, data);
  expect(await readFile(join(agentDir, "MEMORY.md"), "utf8")).toBe("remember this");
  expect(await readdir(agentDir)).toEqual(["MEMORY.md"]);
});

test("reports unwritable shared memory before starting the model", async () => {
  if (process.getuid?.() === 0) return; // root bypasses ordinary POSIX directory permissions.
  const root = await workspace();
  const memoryRoot = join(root, ".claude/agent-memory");
  await mkdir(memoryRoot, { recursive: true });
  await chmod(memoryRoot, 0o500);
  try { await expect(prepareProjectMemory(root, data)).rejects.toThrow("project_memory_unavailable"); }
  finally { await chmod(memoryRoot, 0o700); }
});

test.each([".claude", ".claude/agent-memory", ".claude/agent-memory/anban-article"])("rejects a symlink at %s", async (unsafePath) => {
  const root = await workspace();
  const target = await workspace();
  const parts = unsafePath.split("/");
  if (parts.length > 1) await mkdir(join(root, ...parts.slice(0, -1)), { recursive: true });
  await symlink(target, join(root, unsafePath));
  await expect(prepareProjectMemory(root, data)).rejects.toThrow("project_memory_unavailable");
  expect(await readdir(target)).toEqual([]);
});

test("uses Montage memory mounted directly beneath its runtime working directory", async () => {
  const root = await workspace();
  const memoryRoot = join(root, "openmontage/.claude/agent-memory");
  await mkdir(memoryRoot, { recursive: true });
  const montage = { ...data, agent_flag: "anban:montage", runtime_adapter: "openmontage" } as const;
  await prepareProjectMemory(root, montage);
  await writeFile(join(memoryRoot, "anban-montage/MEMORY.md"), "shared");
  await prepareProjectMemory(root, montage);
  expect(await readFile(join(memoryRoot, "anban-montage/MEMORY.md"), "utf8")).toBe("shared");
  expect(await readdir(root)).toEqual(["openmontage"]);
});

test("rejects a Montage memory root outside its runtime working directory", async () => {
  const root = await workspace();
  await mkdir(join(root, ".claude/agent-memory"), { recursive: true });
  await mkdir(join(root, "openmontage/.claude"), { recursive: true });
  await expect(prepareProjectMemory(root, { ...data, runtime_adapter: "openmontage" })).rejects.toThrow("project_memory_unavailable");
});

test("clean policy recovery does not load or create project memory", async () => {
  const root = await workspace();
  await prepareProjectMemory(root, { ...data, agent_memory_directory: undefined });
  expect(await readdir(root)).toEqual([]);
});
