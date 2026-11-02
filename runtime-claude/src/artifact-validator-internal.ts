import type { Stats } from "node:fs";
import { lstat, open, realpath } from "node:fs/promises";
import { isAbsolute, relative, resolve, sep } from "node:path";

export interface ArtifactValidatorDependencies {
  lstat: typeof lstat;
  open: typeof open;
  realpath: typeof realpath;
  beforeOpen?: (path: string) => void | Promise<void>;
}

export const defaultArtifactValidatorDependencies: ArtifactValidatorDependencies = { lstat, open, realpath };

export async function readValidatedArtifactInternal(
  workspace: string,
  artifactPath: string,
  dependencies: ArtifactValidatorDependencies = defaultArtifactValidatorDependencies,
): Promise<Buffer | undefined> {
  const { target, outputRoot, workspaceRoot } = safeArtifactPaths(workspace, artifactPath);
  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    const before = await dependencies.lstat(target);
    if (!before.isFile() || before.isSymbolicLink()) return undefined;
    const [actualWorkspace, actualOutput, actualTarget] = await Promise.all([
      dependencies.realpath(resolve(workspace)),
      dependencies.realpath(outputRoot),
      dependencies.realpath(target),
    ]);
    if (!isStrictlyInside(actualWorkspace, actualOutput) || !isStrictlyInside(actualOutput, actualTarget)) return undefined;
    await dependencies.beforeOpen?.(target);
    handle = await dependencies.open(target, "r");
    const opened = await handle.stat();
    if (!sameSnapshot(before, opened) || opened.size === 0) return undefined;
    const content = await handle.readFile();
    const openedAfter = await handle.stat();
    const visible = await dependencies.lstat(target);
    const actualVisible = await dependencies.realpath(target);
    if (!sameSnapshot(opened, openedAfter) || !sameSnapshot(opened, visible) || !isStrictlyInside(actualOutput, actualVisible)) return undefined;
    return content;
  } catch {
    return undefined;
  } finally {
    await handle?.close();
  }
}

export async function validateRequiredArtifactInternal(
  workspace: string,
  artifactPath: string,
  dependencies: ArtifactValidatorDependencies = defaultArtifactValidatorDependencies,
): Promise<boolean> {
  const { target, outputRoot, workspaceRoot } = safeArtifactPaths(workspace, artifactPath);

  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    const before = await dependencies.lstat(target);
    if (!before.isFile() || before.isSymbolicLink()) return false;

    const [actualWorkspace, actualOutput, actualTarget] = await Promise.all([
      dependencies.realpath(workspaceRoot),
      dependencies.realpath(outputRoot),
      dependencies.realpath(target),
    ]);
    if (!isStrictlyInside(actualWorkspace, actualOutput) || !isStrictlyInside(actualOutput, actualTarget)) return false;

    await dependencies.beforeOpen?.(target);
    handle = await dependencies.open(target, "r");
    const opened = await handle.stat();
    if (!sameSnapshot(before, opened) || opened.size === 0) return false;

    const actualVisible = await dependencies.realpath(target);
    if (!isStrictlyInside(actualOutput, actualVisible)) return false;
    const [openedAfter, visible] = await Promise.all([handle.stat(), dependencies.lstat(target)]);
    return sameSnapshot(opened, openedAfter) && sameSnapshot(opened, visible);
  } catch {
    return false;
  } finally {
    await handle?.close();
  }
}

function safeArtifactPaths(workspace: string, artifactPath: string): { target: string; outputRoot: string; workspaceRoot: string } {
  const workspaceRoot = resolve(workspace);
  const outputRoot = resolve(workspaceRoot, "output");
  const target = resolve(workspaceRoot, artifactPath);
  const outputRelative = relative(outputRoot, target);
  const canonical = relative(workspaceRoot, target).split(sep).join("/");
  if (!artifactPath
    || isAbsolute(artifactPath)
    || artifactPath.includes("\\")
    || canonical !== artifactPath
    || !outputRelative
    || outputRelative === ".."
    || outputRelative.startsWith(`..${sep}`)
    || isAbsolute(outputRelative)) {
    throw new Error("artifact path escapes workspace");
  }
  return { target, outputRoot, workspaceRoot };
}

function sameSnapshot(left: Stats, right: Stats): boolean {
  return left.isFile()
    && right.isFile()
    && !left.isSymbolicLink()
    && !right.isSymbolicLink()
    && left.size === right.size
    && left.mtimeMs === right.mtimeMs
    && left.dev === right.dev
    && left.ino === right.ino;
}

function isStrictlyInside(root: string, target: string): boolean {
  const path = relative(root, target);
  return Boolean(path) && path !== ".." && !path.startsWith(`..${sep}`) && !isAbsolute(path);
}
