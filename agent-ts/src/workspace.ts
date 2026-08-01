import { chmod, cp, lstat, mkdir, readFile, rename, rm, symlink, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";

import { cleanBootstrapPath, preflightBootstrapFiles, readBoundedText, workspacePath, type BootstrapResponse } from "./bootstrap.js";

const MAX_BOOTSTRAP_FILE_BYTES = 64 << 20;

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
    await cp(template, runtime, { recursive: true, dereference: false, filter: (path) => !path.endsWith("/.git") && !path.includes("/.git/") });
  }
  const outputLink = join(runtime, "output");
  try {
    const info = await lstat(outputLink);
    if (!info.isSymbolicLink()) throw new Error("Montage output must link to canonical output");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    await symlink(join(workspace, "output"), outputLink);
  }
  for (const name of [".anban-creator", "montage-input.json", "montage-tool-policy.json", "montage-pipeline-defaults.json"]) {
    const source = join(workspace, name);
    try { await lstat(source); } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") continue; throw error; }
    const target = join(runtime, name);
    try { await lstat(target); throw new Error(`Montage task input conflicts with ${name}`); } catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
    await rename(source, target);
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

export async function materializeBootstrapFiles(workspace: string, files: BootstrapResponse["files"], signal?: AbortSignal): Promise<void> {
  await ensureRealDirectory(workspace, "workspace root", false);
  for (const file of preflightBootstrapFiles(files ?? [])) {
    const target = workspacePath(workspace, file.path);
    await ensureRealParent(workspace, dirname(target));
    let contents: string;
    if (file.text !== undefined) {
      contents = file.text;
    } else {
      const response = await fetch(file.download_url!, { signal, redirect: "error" });
      if (!response.ok) throw new Error(`download bootstrap file ${file.path} failed: HTTP ${response.status}`);
      contents = await readBoundedText(response, Math.min(file.max_bytes ?? MAX_BOOTSTRAP_FILE_BYTES, MAX_BOOTSTRAP_FILE_BYTES), `bootstrap file ${file.path}`);
    }
    if (file.expected_size !== undefined && Buffer.byteLength(contents) !== file.expected_size) throw new Error(`bootstrap file ${file.path} size mismatch`);
    await writeAtomicRegularFile(target, contents, file.mode);
  }
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

async function writeAtomicRegularFile(target: string, contents: string, mode: number): Promise<void> {
  try {
    const existing = await lstat(target);
    if (existing.isSymbolicLink() || !existing.isFile()) throw new Error(`bootstrap target is not a regular file: ${target}`);
    throw new Error(`bootstrap target conflicts with existing file: ${target}`);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
  const staging = `${target}.anban-stage-${process.pid}-${crypto.randomUUID()}`;
  try {
    await writeFile(staging, contents, { mode, flag: "wx" });
    await chmod(staging, mode);
    await rename(staging, target);
  } catch (error) {
    await rm(staging, { force: true });
    throw error;
  }
}
