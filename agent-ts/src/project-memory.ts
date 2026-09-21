import { randomUUID } from "node:crypto";
import { lstat, mkdir, open, unlink } from "node:fs/promises";
import { join } from "node:path";
import type { BootstrapResponse } from "./bootstrap.js";

export const AGENT_MEMORY_DIRECTORY = ".claude/agent-memory";

export function agentMemoryPath(workspace: string, agentFlag: string): string {
  // Claude Code namespaces plugin Agent memory by the sanitized agent type.
  return join(workspace, AGENT_MEMORY_DIRECTORY, agentFlag.replace(/[^a-zA-Z0-9_-]/g, "-"));
}

export class ProjectMemoryError extends Error {
  constructor(cause: unknown) {
    super(`project_memory_unavailable: ${cause instanceof Error ? cause.message : String(cause)}`, { cause });
    this.name = "ProjectMemoryError";
  }
}

async function requireDirectory(path: string): Promise<void> {
  const info = await lstat(path);
  if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`${path} must be a real directory`);
}

async function ensureDirectory(path: string): Promise<void> {
  try { await mkdir(path, { mode: 0o700 }); }
  catch (error) { if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error; }
  await requireDirectory(path);
}

/** The runtime must mount memory before bootstrapping; never fall back to task-local storage. */
export async function prepareProjectMemory(workspace: string, data: Pick<BootstrapResponse, "agent_memory_directory" | "agent_flag" | "runtime_adapter">): Promise<void> {
  if (!data.agent_memory_directory) return; // Explicit clean provider-policy recovery session.
  try {
    const cwd = data.runtime_adapter === "openmontage" ? join(workspace, "openmontage") : workspace;
    await requireDirectory(cwd);
    const memoryRoot = join(cwd, AGENT_MEMORY_DIRECTORY);
    await requireDirectory(join(cwd, ".claude"));
    await requireDirectory(memoryRoot);
    const agentDir = agentMemoryPath(cwd, data.agent_flag);
    await ensureDirectory(agentDir);
    const probe = join(agentDir, `.anban-write-check-${randomUUID()}`);
    const file = await open(probe, "wx", 0o600);
    try { await file.close(); } finally { await unlink(probe); }
  } catch (error) {
    throw new ProjectMemoryError(error);
  }
}
