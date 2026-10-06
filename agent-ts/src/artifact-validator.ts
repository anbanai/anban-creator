import { glob, lstat } from "node:fs/promises";
import { resolve, sep } from "node:path";

import { validateRequiredArtifactInternal } from "./artifact-validator-internal.js";
import type { AgentPack } from "./bootstrap.js";

const FAILURE_STATE_PATH = "output/failure-state.json";

export type ArtifactValidationResult =
  | { ok: true }
  | { ok: false; reason: "failure_state"; path: string }
  | { ok: false; reason: "missing_artifacts"; missing: string[] };

export async function validateRequiredArtifact(workspace: string, artifactPath: string): Promise<boolean> {
  return validateRequiredArtifactInternal(workspace, artifactPath);
}

export async function validateFinalArtifacts(pack: AgentPack, workspace: string): Promise<ArtifactValidationResult> {
  return validateArtifacts(pack.artifacts.filter((artifact) => artifact.required).map((artifact) => artifact.path), workspace);
}

async function validateArtifacts(required: string[], workspace: string): Promise<ArtifactValidationResult> {
  if (await failureStateExists(workspace)) {
    return { ok: false, reason: "failure_state", path: FAILURE_STATE_PATH };
  }

  const missing: string[] = [];
  for (const artifactPath of required) {
    if (!await validateRequiredArtifactDeclaration(workspace, artifactPath)) missing.push(artifactPath);
  }
  return missing.length > 0 ? { ok: false, reason: "missing_artifacts", missing } : { ok: true };
}

async function validateRequiredArtifactDeclaration(workspace: string, artifactPath: string): Promise<boolean> {
  if (!hasGlobMagic(artifactPath)) return validateRequiredArtifact(workspace, artifactPath);

  // Reuse the exact-path validator once to enforce the workspace/output
  // boundary before asking the filesystem to expand the pattern. The pattern
  // itself normally does not exist, so its boolean result is intentionally
  // ignored; unsafe paths still throw as they do for literal declarations.
  await validateRequiredArtifact(workspace, artifactPath);

  const matches = new Set<string>();
  try {
    for await (const match of glob(artifactPath, { cwd: resolve(workspace) })) {
      const normalized = match.split(sep).join("/");
      // A previous runtime workaround created a file literally named
      // `image_*.png`. It must never satisfy the glob it was meant to stand in
      // for; only concrete paths discovered by expansion are valid.
      if (normalized !== artifactPath) matches.add(normalized);
    }
  } catch {
    return false;
  }

  if (matches.size === 0) return false;
  for (const match of [...matches].sort()) {
    if (!await validateRequiredArtifact(workspace, match)) return false;
  }
  return true;
}

function hasGlobMagic(artifactPath: string): boolean {
  return /[*?\[]/.test(artifactPath);
}

async function failureStateExists(workspace: string): Promise<boolean> {
  try {
    await lstat(resolve(workspace, FAILURE_STATE_PATH));
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code !== "ENOENT";
  }
}
