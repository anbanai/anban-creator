import { createHash } from "node:crypto";
import { chmod, cp, lstat, mkdir, mkdtemp, readFile, readlink, readdir, rename, rm, symlink, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";

import { cleanBootstrapPath, preflightBootstrapFiles, readBoundedText, workspacePath, type BootstrapResponse } from "./bootstrap.js";

const MAX_BOOTSTRAP_FILE_BYTES = 64 << 20;
const MAX_BOOTSTRAP_TOTAL_BYTES = 512 << 20;

type BootstrapCommit = {
  kind: "create" | "replace";
  target: string;
  staged: string;
  backup?: string;
};

export async function prepareWorkspace(workspace: string, taskType: string, runtimeAdapter: BootstrapResponse["runtime_adapter"]): Promise<void> {
  await ensureRealDirectory(workspace, "workspace root", false);
  await ensureRealDirectory(join(workspace, "output"), "output", true);
  if (taskType === "live-slicer") await ensureRealDirectory(join(workspace, "output", "exports", ".parts"), "live-slicer parts", true);
  if (runtimeAdapter === "openmontage") await prepareMontageWorkspace(workspace);
}

async function prepareMontageWorkspace(workspace: string): Promise<void> {
  const runtime = join(workspace, "openmontage");
  try {
    await ensureRealDirectory(runtime, "Montage workspace", false);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    const template = process.env.ANBAN_MONTAGE_TEMPLATE_PATH;
    if (!template) throw new Error("Montage template is unavailable; set ANBAN_MONTAGE_TEMPLATE_PATH");
    await ensureRealDirectory(template, "Montage template", false);
    const stagingRoot = await mkdtemp(join(workspace, ".montage-init-"));
    const staging = join(stagingRoot, "openmontage");
    try {
      await cp(template, staging, { recursive: true, dereference: false, filter: (path) => !path.endsWith("/.git") && !path.includes("/.git/") });
      await makeWritableTree(staging);
      await rename(staging, runtime);
    } catch (copyError) {
      try {
        await ensureRealDirectory(runtime, "Montage workspace", false);
      } catch {
        throw copyError;
      }
    } finally {
      await rm(stagingRoot, { recursive: true, force: true });
    }
  }
  const outputLink = join(runtime, "output");
  try {
    const info = await lstat(outputLink);
    if (!info.isSymbolicLink()) throw new Error("Montage output must link to canonical output");
    if (resolve(runtime, await readlink(outputLink)) !== resolve(workspace, "output")) throw new Error("Montage output must link to canonical output");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    await symlink(join(workspace, "output"), outputLink);
  }
  for (const name of [".anban-creator", "montage-input.json", "montage-tool-policy.json", "montage-pipeline-defaults.json"]) {
    const source = join(workspace, name);
    try { await lstat(source); } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") continue; throw error; }
    await mergeWritableEntry(source, join(runtime, name));
    await rm(source, { recursive: true, force: true });
  }
  const instructions = join(workspace, "CLAUDE.md");
  try {
    const source = await readFile(instructions, "utf8");
    const target = join(runtime, "CLAUDE.md");
    let existing = "";
    try { existing = await readFile(target, "utf8"); } catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
    const section = `\n\n# Anban Project Instructions\n\n${source}`;
    if (!existing.includes(section)) await writeFile(target, `${existing}${section}`, { mode: 0o644 });
    await rm(instructions);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
}

async function makeWritableTree(path: string): Promise<void> {
  const info = await lstat(path);
  if (info.isSymbolicLink()) return;
  if (info.isDirectory()) {
    await chmod(path, (info.mode & 0o777) | 0o700);
    for (const entry of await readdir(path)) await makeWritableTree(join(path, entry));
    return;
  }
  if (info.isFile()) {
    await chmod(path, (info.mode & 0o777) | 0o600);
    return;
  }
  throw new Error(`unsupported file type ${path}`);
}

export async function materializeBootstrapFiles(workspace: string, files: BootstrapResponse["files"], signal?: AbortSignal): Promise<void> {
  await ensureRealDirectory(workspace, "workspace root", false);
  const prepared = preflightBootstrapFiles(files ?? []);
  const stagingRoot = await mkdtemp(join(workspace, ".anban-bootstrap-"));
  const incoming = join(stagingRoot, "incoming");
  const backups = join(stagingRoot, "backups");
  await mkdir(incoming, { mode: 0o700 });
  await mkdir(backups, { mode: 0o700 });
  const commits: BootstrapCommit[] = [];
  const applied: BootstrapCommit[] = [];
  let preserveStaging = false;
  try {
    let totalBytes = 0;
    for (const file of prepared) {
      signal?.throwIfAborted();
      const staged = workspacePath(incoming, file.path);
      await mkdir(dirname(staged), { recursive: true, mode: 0o750 });
      let contents: Uint8Array;
      if (file.text !== undefined) {
        contents = Buffer.from(file.text);
      } else {
        const response = await fetch(file.download_url!, { signal, redirect: "error" });
        if (!response.ok) throw new Error(`download bootstrap file ${file.path} failed: HTTP ${response.status}`);
        contents = await readBoundedBytes(response, Math.min(file.max_bytes ?? MAX_BOOTSTRAP_FILE_BYTES, MAX_BOOTSTRAP_FILE_BYTES), `bootstrap file ${file.path}`);
        if (file.content_sha256 !== undefined && createHash("sha256").update(contents).digest("hex") !== file.content_sha256) {
          throw new Error(`bootstrap file ${file.path} SHA-256 mismatch`);
        }
      }
      totalBytes += contents.byteLength;
      if (totalBytes > MAX_BOOTSTRAP_TOTAL_BYTES) throw new Error("bootstrap files exceed total size limit");
      if (file.expected_size !== undefined && contents.byteLength !== file.expected_size) throw new Error(`bootstrap file ${file.path} size mismatch`);
      await writeFile(staged, contents, { mode: file.mode, flag: "wx" });
      await chmod(staged, file.mode);
    }

    for (const file of prepared) {
      const target = workspacePath(workspace, file.path);
      const staged = workspacePath(incoming, file.path);
      await ensureRealParent(workspace, dirname(target));
      try {
        const existing = await lstat(target);
        if (existing.isSymbolicLink() || !existing.isFile()) throw new Error(`bootstrap target is not a regular file: ${target}`);
        if (await filesEqual(staged, target)) continue;
        if (!file.replace_existing) throw new Error(`bootstrap target conflicts with existing file: ${target}`);
        commits.push({ kind: "replace", target, staged, backup: join(backups, String(commits.length)) });
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
        commits.push({ kind: "create", target, staged });
      }
    }

    for (const commit of commits) {
      signal?.throwIfAborted();
      if (commit.kind === "create") {
        await rename(commit.staged, commit.target);
        applied.push(commit);
      } else {
        await rename(commit.target, commit.backup!);
        applied.push(commit);
        signal?.throwIfAborted();
        await rename(commit.staged, commit.target);
      }
    }
  } catch (error) {
    try {
      await rollbackBootstrapCommits(applied);
    } catch (rollbackError) {
      preserveStaging = true;
      throw new AggregateError([error, rollbackError], `bootstrap materialization failed and rollback was incomplete; recovery files preserved at ${stagingRoot}`);
    }
    throw error;
  } finally {
    if (!preserveStaging) await rm(stagingRoot, { recursive: true, force: true });
  }
}

async function rollbackBootstrapCommits(commits: BootstrapCommit[]): Promise<void> {
  const errors: unknown[] = [];
  for (const commit of [...commits].reverse()) {
    try {
      if (commit.kind === "create") {
        await rm(commit.target, { force: true });
        continue;
      }
      await rm(commit.target, { force: true });
      await rename(commit.backup!, commit.target);
    } catch (error) {
      errors.push(error);
    }
  }
  if (errors.length > 0) throw new AggregateError(errors, "one or more bootstrap rollback operations failed");
}

async function ensureRealDirectory(path: string, label: string, create: boolean): Promise<void> {
  try {
    const info = await lstat(path);
    if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`${label} must be a real directory`);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT" || !create) throw error;
    await mkdir(path, { recursive: true, mode: 0o750 });
    const info = await lstat(path);
    if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`${label} must be a real directory`);
  }
}

async function ensureRealParent(workspace: string, parent: string): Promise<void> {
  const root = resolve(workspace);
  const path = resolve(parent);
  const rel = relative(root, path);
  if (rel === ".." || rel.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`) || rel === "") {
    if (rel === "") return;
    throw new Error("bootstrap parent escapes workspace");
  }
  let current = root;
  for (const component of rel.split(/[\\/]/)) {
    if (!component) continue;
    current = join(current, component);
    await ensureRealDirectory(current, `bootstrap parent ${cleanBootstrapPath(relative(root, current).replaceAll("\\", "/"))}`, true);
  }
}

async function readBoundedBytes(response: Response, limit: number, label: string): Promise<Uint8Array> {
  const reader = response.body?.getReader();
  if (!reader) return new Uint8Array();
  const chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > limit) throw new Error(`${label} exceeds size limit`);
    chunks.push(value);
  }
  return Buffer.concat(chunks);
}

async function filesEqual(first: string, second: string): Promise<boolean> {
  const [left, right] = await Promise.all([readFile(first), readFile(second)]);
  return left.equals(right);
}

async function mergeWritableEntry(source: string, target: string): Promise<void> {
  const sourceInfo = await lstat(source);
  if (sourceInfo.isDirectory() && !sourceInfo.isSymbolicLink()) {
    try {
      const targetInfo = await lstat(target);
      if (!targetInfo.isDirectory() || targetInfo.isSymbolicLink()) throw new Error(`destination ${target} is not a real directory`);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
      await mkdir(target, { mode: sourceInfo.mode | 0o700 });
    }
    for (const entry of await readdir(source)) await mergeWritableEntry(join(source, entry), join(target, entry));
    return;
  }
  try {
    const targetInfo = await lstat(target);
    if (sourceInfo.isFile() && targetInfo.isFile() && !sourceInfo.isSymbolicLink() && !targetInfo.isSymbolicLink() && await filesEqual(source, target)) return;
    if (sourceInfo.isSymbolicLink() && targetInfo.isSymbolicLink() && await readlink(source) === await readlink(target)) return;
    throw new Error(`destination ${target} conflicts with workspace input`);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
  if (sourceInfo.isFile() && !sourceInfo.isSymbolicLink()) {
    await cp(source, target, { force: false, errorOnExist: true, preserveTimestamps: true });
    await chmod(target, sourceInfo.mode | 0o600);
    return;
  }
  if (sourceInfo.isSymbolicLink()) {
    await symlink(await readlink(source), target);
    return;
  }
  throw new Error(`unsupported file type ${source}`);
}
