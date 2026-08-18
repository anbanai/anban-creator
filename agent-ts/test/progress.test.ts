import { describe, expect, mock, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { AgentPack } from "../src/bootstrap.js";
import { ProgressEmitter } from "../src/progress.js";
import type { StageProgressEvent } from "../src/reporter.js";

const articlePack: AgentPack = {
  id: "article",
  version: "1.0.0",
  digest: "a".repeat(64),
  agent: { name: "article" },
  bindings: { task_types: ["article"] },
  runtime: { profile: "article", adapter: "standard" },
  progress: [
    {
      id: "research",
      title: "Research",
      active_percent: 10,
      complete_percent: 20,
      required_artifacts: ["output/topic-analysis.md"],
    },
    { id: "delivery", title: "Delivery", active_percent: 80, complete_percent: 100 },
  ],
};

async function populatedWorkspace(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "anban-progress-emitter-"));
  await mkdir(join(root, "output"));
  await writeFile(join(root, "output", "topic-analysis.md"), "research");
  return root;
}

describe("ProgressEmitter", () => {
  test("maps active metadata to the declared active percent", async () => {
    const root = await populatedWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter(articlePack, { stageProgress });
    try {
      await emitter.handle({ stage: "research", state: "active" }, root);

      expect(stageProgress).toHaveBeenCalledWith({
        stage: "research",
        state: "active",
        title: "Research",
        description: undefined,
        progress_percent: 10,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("maps completion to the declared complete percent after artifact validation", async () => {
    const root = await populatedWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter(articlePack, { stageProgress });
    try {
      const result = await emitter.handle({ stage: "research", state: "complete", description: "done" }, root);

      expect(result).toEqual({ emitted: true, validation: { ok: true } });
      expect(stageProgress).toHaveBeenCalledWith({
        stage: "research",
        state: "complete",
        title: "Research",
        description: "done",
        progress_percent: 20,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("does not emit completion when a required artifact is missing", async () => {
    const root = await mkdtemp(join(tmpdir(), "anban-progress-emitter-"));
    await mkdir(join(root, "output"));
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter(articlePack, { stageProgress });
    try {
      expect(await emitter.handle({ stage: "research", state: "complete" }, root)).toEqual({
        emitted: false,
        validation: { ok: false, reason: "missing_artifacts", missing: ["output/topic-analysis.md"] },
      });
      expect(stageProgress).not.toHaveBeenCalled();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("deduplicates repeated completion events", async () => {
    const root = await populatedWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter(articlePack, { stageProgress });
    try {
      await emitter.handle({ stage: "research", state: "complete" }, root);
      await emitter.handle({ stage: "research", state: "complete" }, root);

      expect(stageProgress).toHaveBeenCalledTimes(1);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("serializes concurrent progress sends", async () => {
    const root = await populatedWorkspace();
    const calls: string[] = [];
    let releaseActive: (() => void) | undefined;
    const activePending = new Promise<void>((resolve) => { releaseActive = resolve; });
    const emitter = new ProgressEmitter(articlePack, {
      stageProgress: async (event) => {
        calls.push(`${event.stage}:${event.state}:start`);
        if (event.state === "active") await activePending;
        calls.push(`${event.stage}:${event.state}:end`);
      },
    });
    try {
      const active = emitter.handle({ stage: "research", state: "active" }, root);
      const complete = emitter.handle({ stage: "research", state: "complete" }, root);
      await Bun.sleep(10);
      expect(calls).toEqual(["research:active:start"]);

      releaseActive?.();
      await Promise.all([active, complete]);
      expect(calls).toEqual([
        "research:active:start",
        "research:active:end",
        "research:complete:start",
        "research:complete:end",
      ]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("diagnoses and ignores an unknown stage", async () => {
    const root = await populatedWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const diagnostics: string[] = [];
    const emitter = new ProgressEmitter(articlePack, { stageProgress }, (message) => diagnostics.push(message));
    try {
      expect(await emitter.handle({ stage: "unknown", state: "active" }, root)).toEqual({ emitted: false });
      expect(stageProgress).not.toHaveBeenCalled();
      expect(diagnostics.join("\n")).toContain("unknown progress stage: unknown");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("swallows reporter failures and emits a diagnostic", async () => {
    const root = await populatedWorkspace();
    const diagnostics: string[] = [];
    const emitter = new ProgressEmitter(
      articlePack,
      { stageProgress: async () => { throw new Error("server unavailable"); } },
      (message) => diagnostics.push(message),
    );
    try {
      await expect(emitter.handle({ stage: "research", state: "active" }, root)).resolves.toEqual({ emitted: false });
      expect(diagnostics.join("\n")).toContain("server unavailable");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("continues the serialized queue after a reporter failure", async () => {
    const root = await populatedWorkspace();
    const calls: StageProgressEvent[] = [];
    let attempts = 0;
    const emitter = new ProgressEmitter(articlePack, {
      stageProgress: async (event) => {
        attempts += 1;
        if (attempts === 1) throw new Error("server unavailable");
        calls.push(event);
      },
    }, () => {});
    try {
      await emitter.handle({ stage: "research", state: "active" }, root);
      await emitter.handle({ stage: "delivery", state: "active" }, root);

      expect(calls.map((event) => event.stage)).toEqual(["delivery"]);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("swallows artifact validator exceptions and emits a diagnostic", async () => {
    const root = await populatedWorkspace();
    const diagnostics: string[] = [];
    const unsafePack: AgentPack = {
      ...articlePack,
      progress: [{
        ...articlePack.progress[0]!,
        complete_percent: 100,
        required_artifacts: ["../secret.md"],
      }],
    };
    const emitter = new ProgressEmitter(unsafePack, { stageProgress: async () => {} }, (message) => diagnostics.push(message));
    try {
      await expect(emitter.handle({ stage: "research", state: "complete" }, root)).resolves.toEqual({ emitted: false });
      expect(diagnostics.join("\n")).toContain("artifact path escapes workspace");
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test("maps final to the last declared stage at 100 percent with complete state", async () => {
    const root = await populatedWorkspace();
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter(articlePack, { stageProgress });
    try {
      await emitter.handle({ state: "final" }, root);

      expect(stageProgress).toHaveBeenCalledWith({
        stage: "delivery",
        state: "complete",
        title: "Delivery",
        description: undefined,
        progress_percent: 100,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
