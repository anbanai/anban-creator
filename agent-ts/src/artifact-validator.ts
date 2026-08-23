import { lstat } from "node:fs/promises";
import { resolve } from "node:path";

import { validateRequiredArtifactInternal } from "./artifact-validator-internal.js";
import type { AgentPack, AgentPackProgressStage } from "./bootstrap.js";

const FAILURE_STATE_PATH = "output/failure-state.json";

export type ArtifactValidationResult =
  | { ok: true }
  | { ok: false; reason: "failure_state"; path: string }
  | { ok: false; reason: "missing_artifacts"; missing: string[] };

export async function validateRequiredArtifact(workspace: string, artifactPath: string): Promise<boolean> {
  return validateRequiredArtifactInternal(workspace, artifactPath);
}

export async function validateStageArtifacts(pack: AgentPack, stageID: string, workspace: string): Promise<ArtifactValidationResult> {
  const stage = pack.progress.find((candidate) => candidate.id === stageID);
  if (!stage) throw new Error(`unknown progress stage: ${stageID}`);
  return validateArtifacts(stage.required_artifacts ?? [], workspace);
}

export async function validateAllStageArtifacts(pack: AgentPack, workspace: string): Promise<ArtifactValidationResult> {
  return validateArtifacts(requiredArtifacts(pack.progress), workspace);
}

export const validateFinalArtifacts = validateAllStageArtifacts;

async function validateArtifacts(required: string[], workspace: string): Promise<ArtifactValidationResult> {
  if (await failureStateExists(workspace)) {
    return { ok: false, reason: "failure_state", path: FAILURE_STATE_PATH };
  }

  const missing: string[] = [];
  for (const artifactPath of required) {
    if (!await validateRequiredArtifact(workspace, artifactPath)) missing.push(artifactPath);
  }
  return missing.length > 0 ? { ok: false, reason: "missing_artifacts", missing } : { ok: true };
}

async function failureStateExists(workspace: string): Promise<boolean> {
  try {
    await lstat(resolve(workspace, FAILURE_STATE_PATH));
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code !== "ENOENT";
  }
}

function requiredArtifacts(stages: AgentPackProgressStage[]): string[] {
  const seen = new Set<string>();
  const artifacts: string[] = [];
  for (const stage of stages) {
    for (const artifactPath of stage.required_artifacts ?? []) {
      if (seen.has(artifactPath)) continue;
      seen.add(artifactPath);
      artifacts.push(artifactPath);
    }
  }
  return artifacts;
}
