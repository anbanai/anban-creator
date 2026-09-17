import { describe, expect, mock, test } from "bun:test";

import { ProgressEmitter } from "../src/progress.js";
import type { StageProgressEvent } from "../src/reporter.js";

describe("ProgressEmitter", () => {
  test("forwards an agent-declared stage without a title or percentage", async () => {
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter({ stageProgress });

    await emitter.handle({ stage: "source_review", state: "active", description: "正在核对来源" });

    expect(stageProgress).toHaveBeenCalledWith({
      stage: "source_review",
      state: "active",
      description: "正在核对来源",
    });
  });

  test("deduplicates repeated stage state events", async () => {
    const stageProgress = mock(async (_event: StageProgressEvent) => {});
    const emitter = new ProgressEmitter({ stageProgress });

    await emitter.handle({ stage: "source_review", state: "complete" });
    await emitter.handle({ stage: "source_review", state: "complete" });

    expect(stageProgress).toHaveBeenCalledTimes(1);
  });

  test("forwards a changed latest update while a stage remains active", async () => {
    const events: StageProgressEvent[] = [];
    const emitter = new ProgressEmitter({
      stageProgress: async (event) => { events.push(event); },
    });

    await emitter.handle({ stage: "source_review", state: "active", description: "正在核对来源" });
    await emitter.handle({ stage: "source_review", state: "active", description: "已核对三条核心来源" });

    expect(events).toEqual([
      { stage: "source_review", state: "active", description: "正在核对来源" },
      { stage: "source_review", state: "active", description: "已核对三条核心来源" },
    ]);
  });

  test("forwards a previously seen update again after the stage description changes", async () => {
    const events: StageProgressEvent[] = [];
    const emitter = new ProgressEmitter({
      stageProgress: async (event) => { events.push(event); },
    });

    await emitter.handle({ stage: "source_review", state: "active", description: "正在核对来源" });
    await emitter.handle({ stage: "source_review", state: "active", description: "已核对三条核心来源" });
    await emitter.handle({ stage: "source_review", state: "active", description: "正在核对来源" });

    expect(events).toEqual([
      { stage: "source_review", state: "active", description: "正在核对来源" },
      { stage: "source_review", state: "active", description: "已核对三条核心来源" },
      { stage: "source_review", state: "active", description: "正在核对来源" },
    ]);
  });

  test("serializes concurrent progress sends", async () => {
    const calls: string[] = [];
    let releaseActive: (() => void) | undefined;
    const activePending = new Promise<void>((resolve) => { releaseActive = resolve; });
    const emitter = new ProgressEmitter({
      stageProgress: async (event) => {
        calls.push(`${event.stage}:${event.state}:start`);
        if (event.state === "active") await activePending;
        calls.push(`${event.stage}:${event.state}:end`);
      },
    });

    const active = emitter.handle({ stage: "source_review", state: "active" });
    const complete = emitter.handle({ stage: "source_review", state: "complete" });
    await Bun.sleep(10);
    expect(calls).toEqual(["source_review:active:start"]);

    releaseActive?.();
    await Promise.all([active, complete]);
    expect(calls).toEqual([
      "source_review:active:start",
      "source_review:active:end",
      "source_review:complete:start",
      "source_review:complete:end",
    ]);
  });

  test("continues the queue after a reporter failure", async () => {
    const calls: StageProgressEvent[] = [];
    const diagnostics: string[] = [];
    let attempts = 0;
    const emitter = new ProgressEmitter({
      stageProgress: async (event) => {
        attempts += 1;
        if (attempts === 1) throw new Error("server unavailable");
        calls.push(event);
      },
    }, (message) => diagnostics.push(message));

    expect(await emitter.handle({ stage: "source_review", state: "active" })).toEqual({ emitted: false });
    expect(await emitter.handle({ stage: "source_review", state: "complete" })).toEqual({ emitted: true });
    expect(calls).toEqual([{ stage: "source_review", state: "complete", description: undefined }]);
    expect(diagnostics.join("\n")).toContain("server unavailable");
  });
});
