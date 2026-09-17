import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rename, rm, symlink, writeFile } from "node:fs/promises";
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
  id: "article",
  version: "1.0.0",
  digest: "a".repeat(64),
  agent: { name: "article" },
  bindings: { task_types: ["article"] },
  runtime: { profile: "article", adapter: "standard" },
  artifacts: [
    { role: "analysis", path: "output/topic-analysis.md", required: true },
    { role: "final", path: "output/final.md", required: true },
    { role: "optional", path: "output/review.md", required: false },
  ],
};

const viralAnalysisPack: AgentPack = {
  ...pack,
  id: "seednote",
  agent: { name: "seednote" },
  bindings: { task_types: ["seednote", "viral_analysis"] },
  runtime: { profile: "seednote", adapter: "standard" },
  artifacts: [
    { role: "analysis", path: "output/source-analysis.md", required: true },
    { role: "template", path: "output/viral-template.json", required: true },
  ],
};

async function workspace(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "anban-progress-artifacts-"));
  roots.push(root);
  await mkdir(join(root, "output"), { recursive: true });
  return root;
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
});
