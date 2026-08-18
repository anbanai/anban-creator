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

export async function validateRequiredArtifactInternal(
  workspace: string,
  artifactPath: string,
  dependencies: ArtifactValidatorDependencies = defaultArtifactValidatorDependencies,
): Promise<boolean> {
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
