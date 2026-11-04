import { afterEach, describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rename, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  validateFinalArtifacts,
  validateRequiredArtifact,
} from "../src/artifact-validator.js";
import { defaultArtifactValidatorDependencies, validateRequiredArtifactInternal } from "../src/artifact-validator-internal.js";
import type { AgentPack } from "../src/bootstrap.js";

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

const pack: AgentPack = {
  id: "wechat-article",
  version: "1.0.0",
  digest: "a".repeat(64),
  agent: { name: "wechat-article", max_turns: 60 },
  bindings: { task_types: ["wechat-article"] },
  runtime: { profile: "wechat", adapter: "standard" },
  artifacts: [
    { role: "analysis", path: "output/topic-analysis.md", required: true },
    { role: "final", path: "output/final.md", required: true },
    { role: "optional", path: "output/review.md", required: false },
  ],
};

const viralAnalysisPack: AgentPack = {
  ...pack,
  id: "seednote",
  agent: { name: "seednote", max_turns: 20 },
  bindings: { task_types: ["seednote", "viral_analysis"] },
  runtime: { profile: "seednote", adapter: "standard" },
  artifacts: [
    { role: "analysis", path: "output/source-analysis.md", required: true },
    { role: "template", path: "output/viral-template.json", required: true },
  ],
};

const picturePack: AgentPack = {
  ...pack,
  id: "wechat-picture",
  agent: { name: "wechat-picture", max_turns: 60 },
  bindings: { task_types: ["wechat-picture"] },
  runtime: { profile: "wechat-picture", adapter: "standard" },
  artifacts: [
    { role: "image", path: "output/image_*.png", required: true },
  ],
};

const adaptivePicturePack: AgentPack = {
  ...picturePack,
  artifacts: [
    { role: "cover", path: "output/cover.png", required: true },
    { role: "image", path: "output/image_*.png", required: false },
  ],
};

const talkingHeadPack: AgentPack = {
  ...pack,
  id: "talking-head-editor",
  agent: { name: "talking-head-editor", max_turns: 220 },
  bindings: { task_types: ["talking_head_editor"] },
  artifacts: [
    { role: "final", path: "output/final.mp4", required: true },
    { role: "transcript", path: "output/transcript.json", required: true },
    { role: "plan", path: "output/visual-plan.json", required: true },
  ],
  artifact_stages: [
    {
      id: "motion_review",
      trigger: "output/motion-sample.mp4",
      required: ["output/transcript.json", "output/visual-plan.json", "output/motion-review.json"],
    },
    { id: "final_delivery", trigger: "output/final.mp4", terminal: true },
  ],
};

const explainerPack: AgentPack = {
  ...talkingHeadPack,
  id: "talking-head-explainer",
  artifacts: [
    { role: "final", path: "output/final.mp4", required: true },
    { role: "explainer_input_assessment", path: "output/explainer-input-assessment.json", required: false },
    { role: "semantic_master", path: "output/semantic-master.json", required: false },
    { role: "explanation_rehearsal", path: "output/explanation-rehearsal.json", required: false },
  ],
  artifact_stages: [
    {
      id: "explainer_preflight",
      trigger: "output/explainer-input-assessment.json",
      required: [
        "output/explainer-input-assessment.json",
        "output/semantic-master.json",
        "output/explanation-plan.json",
        "output/explanation-rehearsal.json",
        "output/platform-variant-plan.json",
        "output/retention-experiment-plan.json",
        "output/independent-validation-plan.json",
        "output/independent-validation.json",
      ],
      required_on_final: true,
    },
    { id: "final_delivery", trigger: "output/final.mp4", terminal: true },
  ],
} as AgentPack;

async function workspace(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "anban-progress-artifacts-"));
  roots.push(root);
  await mkdir(join(root, "output"), { recursive: true });
  return root;
}

const semanticMaster = {
  core_claims: ["A"], evidence: ["B"], explanation_order: ["C"], primary_hook: "D",
  spoken_semantics: ["E"], visual_anchors: ["F"], conclusion: "G", qualifications: ["H"],
};

function stableHash(value: unknown): string {
  const sort = (current: unknown): unknown => Array.isArray(current)
    ? current.map(sort)
    : current && typeof current === "object"
      ? Object.fromEntries(Object.keys(current as Record<string, unknown>).sort().map((key) => [key, sort((current as Record<string, unknown>)[key])]))
      : current;
  return createHash("sha256").update(JSON.stringify(sort(value))).digest("hex");
}

async function writeExplainerEvidence(root: string, overrides: Record<string, unknown> = {}): Promise<void> {
  const hash = stableHash(semanticMaster);
  const envelope = (extra: Record<string, unknown>) => ({ schema_version: "1.0.0", status: "ready", source: "ip-explainer", workflow: "talking-head-explainer", data_at: "2026-11-02T00:00:00Z", missing: [], evidence_paths: [], ...extra });
  await writeFile(join(root, "output", "explainer-input-assessment.json"), JSON.stringify(envelope({ inputs: [] })));
  await writeFile(join(root, "output", "semantic-master.json"), JSON.stringify(envelope({ semantic_master: semanticMaster, revision_id: "rev-1", content_sha256: hash })));
  for (const path of [
    "explanation-plan.json",
    "retention-experiment-plan.json",
    "independent-validation-plan.json",
    "independent-validation.json",
  ]) {
    await writeFile(join(root, "output", path), JSON.stringify(envelope({ revision_id: "rev-1", semantic_master_sha256: hash })));
  }
  await writeFile(join(root, "output", "platform-variant-plan.json"), JSON.stringify(envelope({ master_revision_id: "rev-1", semantic_master_sha256: hash, platforms: [] })));
  await writeFile(join(root, "output", "explanation-rehearsal.json"), JSON.stringify(envelope({ revision_id: "rev-1", semantic_master_sha256: hash, rehearsal_mode: "still_frames", units: [{ required: true, passed: true }], collision_checks: [], decision: "proceed", expensive_generation_allowed: true, ...overrides })));
}

describe("artifact validation", () => {
  test("reports a required artifact that is missing", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.md"), "ready");

    expect(await validateFinalArtifacts(pack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/topic-analysis.md"],
    });
  });

  test("reports empty and non-regular required artifacts as missing", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "topic-analysis.md"), "");
    await mkdir(join(root, "output", "final.md"));

    expect(await validateFinalArtifacts(pack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/topic-analysis.md", "output/final.md"],
    });
  });

  test("throws when a declared artifact path escapes workspace output", async () => {
    const root = await workspace();

    await expect(validateRequiredArtifact(root, "../secret.txt")).rejects.toThrow("artifact path escapes workspace");
    await expect(validateRequiredArtifact(root, "output/../secret.txt")).rejects.toThrow("artifact path escapes workspace");
  });

  test("rejects a required artifact reached through an escaping symlink", async () => {
    const root = await workspace();
    const outside = join(root, "outside");
    await mkdir(outside);
    await writeFile(join(outside, "secret.md"), "secret");
    await symlink(outside, join(root, "output", "linked"));

    expect(await validateRequiredArtifact(root, "output/linked/secret.md")).toBe(false);
  });

  test("treats any existing failure-state path as a final failure", async () => {
    for (const kind of ["empty", "directory", "symlink", "malformed", "valid"] as const) {
      const root = await workspace();
      await writeFile(join(root, "output", "topic-analysis.md"), "research");
      await writeFile(join(root, "output", "final.md"), "delivery");
      const failureState = join(root, "output", "failure-state.json");
      if (kind === "directory") await mkdir(failureState);
      else if (kind === "symlink") await symlink(join(root, "missing-target"), failureState);
      else if (kind === "empty") await writeFile(failureState, "");
      else if (kind === "malformed") await writeFile(failureState, "not-json");
      else await writeFile(failureState, "{\"error\":\"quality gate\"}");

      const expected = { ok: false, reason: "failure_state", path: "output/failure-state.json" };
      expect(await validateFinalArtifacts(pack, root)).toEqual(expected);
    }
  });

  test("rejects a file replaced between lstat and open", async () => {
    const root = await workspace();
    const target = join(root, "output", "topic-analysis.md");
    const replacement = join(root, "replacement.md");
    await writeFile(target, "original");
    await writeFile(replacement, "replacement bytes");

    expect(await validateRequiredArtifactInternal(root, "output/topic-analysis.md", {
      ...defaultArtifactValidatorDependencies,
      beforeOpen: async () => { await rename(replacement, target); },
    })).toBe(false);
  });

  test("accepts non-empty regular files across the pack", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "topic-analysis.md"), "research");
    await writeFile(join(root, "output", "final.md"), "delivery");

    expect(await validateFinalArtifacts(pack, root)).toEqual({ ok: true });
  });

  test("validates only the artifacts in the resolved viral analysis contract", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "source-analysis.md"), "analysis");
    await writeFile(join(root, "output", "viral-template.json"), "{}");

    expect(await validateFinalArtifacts(viralAnalysisPack, root)).toEqual({ ok: true });
    expect(await validateFinalArtifacts(pack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/topic-analysis.md", "output/final.md"],
    });
  });

  test("expands a required image glob and validates every concrete match", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "image_01.png"), "first image");
    await writeFile(join(root, "output", "image_02.png"), "second image");

    expect(await validateFinalArtifacts(picturePack, root)).toEqual({ ok: true });
  });

  test("does not accept a literal wildcard compatibility placeholder for a glob", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "image_*.png"), "placeholder");

    expect(await validateFinalArtifacts(picturePack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/image_*.png"],
    });
  });

  test("reports a required glob when no concrete image matches", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "image.txt"), "not an image");

    expect(await validateFinalArtifacts(picturePack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/image_*.png"],
    });
  });

  test("allows an adaptive picture package to contain only its cover", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "cover.png"), "cover image");

    expect(await validateFinalArtifacts(adaptivePicturePack, root)).toEqual({ ok: true });
  });

  test("uses the motion review artifact stage before final delivery", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "transcript.json"), "{}");
    await writeFile(join(root, "output", "visual-plan.json"), "{}");
    await writeFile(join(root, "output", "motion-sample.mp4"), "sample");

    expect(await validateFinalArtifacts(talkingHeadPack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/motion-review.json"],
    });

    await writeFile(join(root, "output", "motion-review.json"), "{}");
    expect(await validateFinalArtifacts(talkingHeadPack, root)).toEqual({ ok: true });
  });

  test("selects the terminal stage when final delivery exists", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeFile(join(root, "output", "transcript.json"), "{}");
    await writeFile(join(root, "output", "visual-plan.json"), "{}");

    expect(await validateFinalArtifacts(talkingHeadPack, root)).toEqual({ ok: true });
  });

  test("keeps triggered explainer gates required when final delivery exists", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeFile(join(root, "output", "explainer-input-assessment.json"), "{}");
    await writeFile(join(root, "output", "semantic-master.json"), "{}");

    expect(await validateFinalArtifacts(explainerPack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: [
        "output/explanation-plan.json",
        "output/explanation-rehearsal.json",
        "output/platform-variant-plan.json",
        "output/retention-experiment-plan.json",
        "output/independent-validation-plan.json",
        "output/independent-validation.json",
      ],
    });
  });

  test("rejects malformed explainer JSON instead of accepting file presence", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeExplainerEvidence(root);
    await writeFile(join(root, "output", "platform-variant-plan.json"), "{}");
    const result = await validateFinalArtifacts(explainerPack, root);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.reason).toBe("invalid_artifacts");
  });

  test("accepts explainer evidence when the primary rehearsal passes", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeExplainerEvidence(root);
    expect(await validateFinalArtifacts(explainerPack, root)).toEqual({ ok: true });
  });

  test("rejects a required explainer artifact marked failed", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeExplainerEvidence(root);
    const planPath = join(root, "output", "explanation-plan.json");
    const plan = JSON.parse(await readFile(planPath, "utf8")) as Record<string, unknown>;
    plan.status = "failed";
    await writeFile(planPath, JSON.stringify(plan));

    const result = await validateFinalArtifacts(explainerPack, root);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toBe("invalid_artifacts");
      if (result.reason === "invalid_artifacts") {
        expect(result.invalid).toContainEqual(expect.objectContaining({
          path: "output/explanation-plan.json",
          errors: expect.arrayContaining(["explainer artifacts cannot have status failed"]),
        }));
      }
    }
  });

  test("rejects stale master bindings in each plan and validation artifact", async () => {
    const stalePaths = [
      "output/explanation-plan.json",
      "output/retention-experiment-plan.json",
      "output/independent-validation-plan.json",
      "output/independent-validation.json",
    ];
    for (const stalePath of stalePaths) {
      const root = await workspace();
      await writeFile(join(root, "output", "final.mp4"), "final");
      await writeExplainerEvidence(root);

      const updatedMaster = { ...semanticMaster, core_claims: ["updated claim"] };
      const updatedHash = stableHash(updatedMaster);
      const masterEnvelope = JSON.parse(await readFile(join(root, "output", "semantic-master.json"), "utf8")) as Record<string, unknown>;
      masterEnvelope.semantic_master = updatedMaster;
      masterEnvelope.revision_id = "rev-2";
      masterEnvelope.content_sha256 = updatedHash;
      await writeFile(join(root, "output", "semantic-master.json"), JSON.stringify(masterEnvelope));

      for (const path of [
        "output/explanation-plan.json",
        "output/explanation-rehearsal.json",
        "output/platform-variant-plan.json",
        "output/retention-experiment-plan.json",
        "output/independent-validation-plan.json",
        "output/independent-validation.json",
      ]) {
        if (path === stalePath) continue;
        const filePath = join(root, path);
        const evidence = JSON.parse(await readFile(filePath, "utf8")) as Record<string, unknown>;
        evidence.revision_id = "rev-2";
        evidence.master_revision_id = "rev-2";
        evidence.semantic_master_sha256 = updatedHash;
        await writeFile(filePath, JSON.stringify(evidence));
      }

      const result = await validateFinalArtifacts(explainerPack, root);
      expect(result.ok, stalePath).toBe(false);
      if (!result.ok) {
        expect(result.reason, stalePath).toBe("invalid_artifacts");
        if (result.reason === "invalid_artifacts") {
          expect(result.invalid.some((entry) => entry.path === stalePath), stalePath).toBe(true);
        }
      }
    }
  });

  test("accepts an applied and reviewed low-cost fallback without permitting expensive generation", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");
    await writeFile(join(root, "output", "fallback-walkthrough.md"), "Static comparison walkthrough passed.");
    await writeExplainerEvidence(root, {
      units: [{
        required: true,
        passed: false,
        applied_fallback: {
          method: "static_comparison",
          applied: true,
          description: "Replace the confusing motion graphic with a two-column comparison.",
          walkthrough_result: "passed",
          evidence_paths: ["output/fallback-walkthrough.md"],
        },
      }],
      decision: "fallback",
      expensive_generation_allowed: false,
    });

    expect(await validateFinalArtifacts(explainerPack, root)).toEqual({ ok: true });
  });

  test("rejects fallback delivery when the fallback is missing, unapplied, unreviewed, or unsupported by evidence", async () => {
    const cases: Array<{ name: string; unit: Record<string, unknown>; evidence?: boolean }> = [
      { name: "missing fallback", unit: { required: true, passed: false } },
      {
        name: "not applied",
        unit: { required: true, passed: false, applied_fallback: { method: "talking_head", applied: false, description: "Use the presenter.", walkthrough_result: "passed", evidence_paths: ["output/fallback-walkthrough.md"] } },
        evidence: true,
      },
      {
        name: "failed fallback walkthrough",
        unit: { required: true, passed: false, applied_fallback: { method: "simple_graphic", applied: true, description: "Use a simpler diagram.", walkthrough_result: "failed", evidence_paths: ["output/fallback-walkthrough.md"] } },
        evidence: true,
      },
      {
        name: "missing evidence file",
        unit: { required: true, passed: false, applied_fallback: { method: "static_comparison", applied: true, description: "Use a static comparison.", walkthrough_result: "passed", evidence_paths: ["output/missing-fallback-evidence.md"] } },
      },
      {
        name: "invalid evidence path",
        unit: { required: true, passed: false, applied_fallback: { method: "other", applied: true, description: "Use a low-risk replacement.", walkthrough_result: "passed", evidence_paths: ["fallback-walkthrough.md"] } },
        evidence: true,
      },
    ];

    for (const scenario of cases) {
      const root = await workspace();
      await writeFile(join(root, "output", "final.mp4"), "final");
      if (scenario.evidence) await writeFile(join(root, "output", "fallback-walkthrough.md"), "Fallback preview.");
      await writeExplainerEvidence(root, {
        units: [scenario.unit],
        decision: "fallback",
        expensive_generation_allowed: false,
      });
      const result = await validateFinalArtifacts(explainerPack, root);
      expect(result.ok, scenario.name).toBe(false);
      if (!result.ok) expect(result.reason, scenario.name).toBe("invalid_artifacts");
    }
  });

  test("rejects inconsistent rehearsal decisions and generation permissions", async () => {
    const cases = [
      { decision: "proceed", expensive_generation_allowed: false, units: [{ required: true, passed: true }] },
      { decision: "proceed", expensive_generation_allowed: true, units: [{ required: true, passed: false }] },
      { decision: "fallback", expensive_generation_allowed: true, units: [{ required: true, passed: true }] },
      { decision: "fallback", expensive_generation_allowed: false, units: [{ required: true, passed: true }] },
      { decision: "blocked", expensive_generation_allowed: false, units: [{ required: true, passed: false }] },
    ];
    for (const scenario of cases) {
      const root = await workspace();
      await writeFile(join(root, "output", "final.mp4"), "final");
      await writeExplainerEvidence(root, scenario);
      const result = await validateFinalArtifacts(explainerPack, root);
      expect(result.ok).toBe(false);
      if (!result.ok) expect(result.reason).toBe("invalid_artifacts");
    }
  });

  test("does not activate explainer-only final gates for ordinary talking-head tasks", async () => {
    const root = await workspace();
    await writeFile(join(root, "output", "final.mp4"), "final");

    expect(await validateFinalArtifacts(explainerPack, root)).toEqual({ ok: true });
  });

  test("does not treat an explainer preflight as final completion", async () => {
    const root = await workspace();
    await writeExplainerEvidence(root);

    expect(await validateFinalArtifacts(explainerPack, root)).toEqual({
      ok: false,
      reason: "missing_artifacts",
      missing: ["output/final.mp4"],
    });
  });
});
