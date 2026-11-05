import { glob, lstat } from "node:fs/promises";
import { createHash } from "node:crypto";
import { resolve, sep } from "node:path";

import { readValidatedArtifactInternal, validateRequiredArtifactInternal } from "./artifact-validator-internal.js";
import type { AgentPack } from "./bootstrap.js";

const FAILURE_STATE_PATH = "output/failure-state.json";

export type ArtifactValidationResult =
  | { ok: true }
  | { ok: false; reason: "failure_state"; path: string }
  | { ok: false; reason: "missing_artifacts"; missing: string[] }
  | { ok: false; reason: "invalid_artifacts"; invalid: Array<{ path: string; errors: string[] }> };

export async function validateRequiredArtifact(workspace: string, artifactPath: string): Promise<boolean> {
  return validateRequiredArtifactInternal(workspace, artifactPath);
}

export async function validateFinalArtifacts(pack: AgentPack, workspace: string): Promise<ArtifactValidationResult> {
  const artifacts = pack.artifacts ?? [];
  const finalRequired = artifacts.filter((artifact) => artifact.required).map((artifact) => artifact.path);
  const stages = pack.artifact_stages ?? [];
  // A review gate is a legitimate managed stop. Select a terminal stage first
  // so a completed delivery is never accepted against an earlier gate.
  for (const terminal of [true, false]) {
    for (const stage of stages) {
      // Supplemental final requirements must not turn an intermediate
      // preflight artifact into a successful task completion on its own.
      if (!terminal && stage.required_on_final) continue;
      if (Boolean(stage.terminal) !== terminal) continue;
      if (!await validateRequiredArtifactDeclaration(workspace, stage.trigger)) continue;
      const required = stage.required?.length ? [...stage.required] : [...finalRequired];
      if (terminal) {
        for (const conditional of stages) {
          if (!conditional.required_on_final) continue;
          if (await validateRequiredArtifactDeclaration(workspace, conditional.trigger)) {
            required.push(...(conditional.required ?? []));
          }
        }
      }
      return validateArtifacts([...new Set(required)], workspace);
    }
  }
  return validateArtifacts(finalRequired, workspace);
}

async function validateArtifacts(required: string[], workspace: string): Promise<ArtifactValidationResult> {
  if (await failureStateExists(workspace)) {
    return { ok: false, reason: "failure_state", path: FAILURE_STATE_PATH };
  }

  const missing: string[] = [];
  for (const artifactPath of required) {
    if (!await validateRequiredArtifactDeclaration(workspace, artifactPath)) missing.push(artifactPath);
  }
  if (missing.length > 0) return { ok: false, reason: "missing_artifacts", missing };

  const explainer = await validateExplainerContracts(required, workspace);
  return explainer.length > 0 ? { ok: false, reason: "invalid_artifacts", invalid: explainer } : { ok: true };
}

const EXPLAINER_WORKFLOW = "talking-head-explainer";
const EXPLAINER_TRIGGER = "output/explainer-input-assessment.json";
const JSON_LIMIT_BYTES = 4 * 1024 * 1024;
const EXPLAINER_JSON_PATHS = new Set([
  EXPLAINER_TRIGGER,
  "output/semantic-master.json",
  "output/explanation-plan.json",
  "output/explanation-rehearsal.json",
  "output/platform-variant-plan.json",
  "output/retention-experiment-plan.json",
  "output/independent-validation-plan.json",
  "output/independent-validation.json",
  "output/retention-results.json",
]);

async function validateExplainerContracts(required: string[], workspace: string): Promise<Array<{ path: string; errors: string[] }>> {
  if (!required.includes(EXPLAINER_TRIGGER) || !await validateRequiredArtifactDeclaration(workspace, EXPLAINER_TRIGGER)) return [];

  const parsed = new Map<string, Record<string, unknown>>();
  const invalid: Array<{ path: string; errors: string[] }> = [];
  for (const path of EXPLAINER_JSON_PATHS) {
    if (!required.includes(path) && !await validateRequiredArtifactDeclaration(workspace, path)) continue;
    const result = await readJsonArtifact(workspace, path);
    if (!result.value) {
      invalid.push({ path, errors: result.errors });
      continue;
    }
    const errors = validateExplainerEnvelope(result.value);
    if (errors.length > 0) invalid.push({ path, errors });
    else parsed.set(path, result.value);
  }

  const master = parsed.get("output/semantic-master.json");
  const platformPlan = parsed.get("output/platform-variant-plan.json");
  const rehearsal = parsed.get("output/explanation-rehearsal.json");
  if (master) {
    const semanticMaster = master.semantic_master;
    const errors: string[] = [];
    if (!isRecord(semanticMaster)) errors.push("semantic_master must be an object");
    else {
      for (const key of ["core_claims", "evidence", "explanation_order", "primary_hook", "spoken_semantics", "visual_anchors", "conclusion", "qualifications"]) {
        if (!(key in semanticMaster)) errors.push(`semantic_master.${key} is required`);
      }
      const expectedHash = stableSha256(semanticMaster);
      if (master.content_sha256 !== expectedHash) errors.push("content_sha256 does not match semantic_master");
    }
    if (typeof master.revision_id !== "string" || !master.revision_id.trim()) errors.push("revision_id is required");
    addInvalid(invalid, "output/semantic-master.json", errors);

    if (platformPlan) {
      const platformErrors: string[] = [];
      if (platformPlan.master_revision_id !== master.revision_id) platformErrors.push("master_revision_id does not match semantic-master revision_id");
      if (platformPlan.semantic_master_sha256 !== master.content_sha256) platformErrors.push("semantic_master_sha256 does not match semantic-master content_sha256");
      addInvalid(invalid, "output/platform-variant-plan.json", platformErrors);
    }
    if (rehearsal) {
      const rehearsalErrors = await validateRehearsal(rehearsal, master, workspace);
      addInvalid(invalid, "output/explanation-rehearsal.json", rehearsalErrors);
    }

    for (const path of [
      "output/explanation-plan.json",
      "output/retention-experiment-plan.json",
      "output/independent-validation-plan.json",
      "output/independent-validation.json",
    ]) {
      const artifact = parsed.get(path);
      if (!artifact) continue;
      const bindingErrors: string[] = [];
      if (artifact.revision_id !== master.revision_id) bindingErrors.push("revision_id does not match semantic-master revision_id");
      if (artifact.semantic_master_sha256 !== master.content_sha256) bindingErrors.push("semantic_master_sha256 does not match semantic-master content_sha256");
      addInvalid(invalid, path, bindingErrors);
    }
  }
  return invalid;
}

async function readJsonArtifact(workspace: string, artifactPath: string): Promise<{ value?: Record<string, unknown>; errors: string[] }> {
  try {
    // Parse the bytes from the same validated file handle used for artifact
    // delivery checks, so a concurrent replacement cannot change the JSON
    // after validation.
    const content = await readValidatedArtifactInternal(workspace, artifactPath);
    if (!content) {
      return { errors: ["artifact is missing, empty, non-regular, or unsafe"] };
    }
    if (content.byteLength > JSON_LIMIT_BYTES) return { errors: [`JSON exceeds ${JSON_LIMIT_BYTES} bytes`] };
    const value: unknown = JSON.parse(content.toString("utf8"));
    return isRecord(value) ? { value, errors: [] } : { errors: ["JSON root must be an object"] };
  } catch (error) {
    return { errors: [`invalid JSON: ${error instanceof Error ? error.message : "read failed"}`] };
  }
}

function validateExplainerEnvelope(value: Record<string, unknown>): string[] {
  const errors: string[] = [];
  if (typeof value.schema_version !== "string" || !value.schema_version.trim()) errors.push("schema_version is required");
  if (value.status === "failed") errors.push("explainer artifacts cannot have status failed");
  if (!(["ready", "partial", "missing", "failed"] as unknown[]).includes(value.status)) errors.push("status must be ready, partial, missing, or failed");
  if (typeof value.source !== "string" || !value.source.trim()) errors.push("source is required");
  if (value.workflow !== EXPLAINER_WORKFLOW) errors.push(`workflow must be ${EXPLAINER_WORKFLOW}`);
  if (typeof value.data_at !== "string" || !value.data_at.trim()) errors.push("data_at is required");
  if (!Array.isArray(value.missing)) errors.push("missing must be an array");
  if (!Array.isArray(value.evidence_paths)) errors.push("evidence_paths must be an array");
  return errors;
}

async function validateRehearsal(value: Record<string, unknown>, master: Record<string, unknown>, workspace: string): Promise<string[]> {
  const errors: string[] = [];
  if (value.revision_id !== master.revision_id) errors.push("revision_id does not match semantic-master revision_id");
  if (value.semantic_master_sha256 !== master.content_sha256) errors.push("semantic_master_sha256 does not match semantic-master content_sha256");
  if (!(new Set(["still_frames", "placeholders", "simple_graphics"])).has(value.rehearsal_mode as string)) errors.push("rehearsal_mode is invalid");
  if (!Array.isArray(value.units) || value.units.length === 0) errors.push("units must be a non-empty array");
  if (!Array.isArray(value.collision_checks)) errors.push("collision_checks must be an array");
  if (!(new Set(["proceed", "fallback", "blocked"])).has(value.decision as string)) errors.push("decision is invalid");
  if (typeof value.expensive_generation_allowed !== "boolean") errors.push("expensive_generation_allowed must be boolean");
  let unresolvedUnits = false;
  if (Array.isArray(value.units)) {
    for (const [index, unit] of value.units.entries()) {
      if (!isRecord(unit)) { errors.push(`units[${index}] must be an object`); continue; }
      const required = unit.required !== false;
      if (!required) continue;
      const passed = unit.passed === true || unit.result === "passed" || unit.walkthrough_result === "passed";
      if (passed) continue;

      unresolvedUnits = true;
      if (value.decision !== "fallback") {
        errors.push(`units[${index}] required walkthrough did not pass`);
        continue;
      }

      const fallback = unit.applied_fallback;
      if (!isRecord(fallback)) {
        errors.push(`units[${index}] applied_fallback is required when the primary walkthrough fails`);
        continue;
      }
      if (!(new Set(["static_comparison", "talking_head", "simple_graphic"])).has(fallback.method as string)) {
        errors.push(`units[${index}] applied_fallback.method is invalid`);
      }
      if (fallback.applied !== true) errors.push(`units[${index}] applied_fallback.applied must be true`);
      if (typeof fallback.description !== "string" || !fallback.description.trim()) {
        errors.push(`units[${index}] applied_fallback.description is required`);
      }
      if (fallback.walkthrough_result !== "passed") {
        errors.push(`units[${index}] applied_fallback.walkthrough_result must be passed`);
      }
      if (!Array.isArray(fallback.evidence_paths) || fallback.evidence_paths.length === 0) {
        errors.push(`units[${index}] applied_fallback.evidence_paths must be a non-empty array`);
      } else {
        for (const [evidenceIndex, evidencePath] of fallback.evidence_paths.entries()) {
          if (typeof evidencePath !== "string" || !evidencePath.startsWith("output/")) {
            errors.push(`units[${index}] applied_fallback.evidence_paths[${evidenceIndex}] must be an output path`);
          } else if (!await validateRequiredArtifactDeclaration(workspace, evidencePath)) {
            errors.push(`units[${index}] applied_fallback.evidence_paths[${evidenceIndex}] is missing or invalid`);
          }
        }
      }
    }
  }
  if (value.decision === "proceed") {
    if (value.expensive_generation_allowed !== true) errors.push("proceed requires expensive_generation_allowed=true");
    if (unresolvedUnits) errors.push("proceed requires every required walkthrough to pass");
  } else if (value.decision === "fallback") {
    if (value.expensive_generation_allowed !== false) errors.push("fallback requires expensive_generation_allowed=false");
    if (!unresolvedUnits) errors.push("fallback requires at least one required primary walkthrough to fail");
  } else if (value.decision === "blocked") {
    if (value.expensive_generation_allowed !== false) errors.push("blocked requires expensive_generation_allowed=false");
    errors.push("blocked rehearsal cannot be delivered");
  }
  return errors;
}

function addInvalid(invalid: Array<{ path: string; errors: string[] }>, path: string, errors: string[]): void {
  if (errors.length === 0) return;
  const existing = invalid.find((entry) => entry.path === path);
  if (existing) existing.errors.push(...errors);
  else invalid.push({ path, errors });
}

function stableSha256(value: unknown): string {
  return createHash("sha256").update(JSON.stringify(sortJson(value))).digest("hex");
}

function sortJson(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortJson);
  if (!isRecord(value)) return value;
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortJson(value[key])]));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
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
