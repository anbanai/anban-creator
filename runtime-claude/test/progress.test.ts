import { describe, expect, mock, test } from "bun:test";

import { BestEffortProgressReporter, ProgressEmitter } from "../src/progress.js";
import { Reporter, type StageProgressEvent } from "../src/reporter.js";
import { TypedTransportError } from "../src/transport.js";

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

describe("BestEffortProgressReporter", () => {
  test("contains exhausted telemetry failures and rate-limits diagnostics", async () => {
    let attempts = 0;
    let reports = 0;
    const diagnostics: string[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => {
      attempts += 1;
      throw Object.assign(new TypeError("fetch failed"), { cause: { code: "EAI_AGAIN" } });
    };
    try {
      const sender = new BestEffortProgressReporter({
        progress: async (message, signal) => {
          reports += 1;
          if (reports === 1) {
            await new Reporter({ serverURL: "https://creator.example.test", executionID: "execution-1" }, "token", "task-1").progress(message, signal);
            return;
          }
          throw new TypedTransportError({ operation: "progress", code: "network_unavailable", attempts: 4, retryable: true }, undefined, undefined, "EAI_AGAIN");
        },
      }, (message) => diagnostics.push(message));

      await sender.report("agent output");

      expect(attempts).toBe(4);
      expect(diagnostics).toEqual([
        "best-effort progress delivery failed operation=progress code=network_unavailable attempts=4 consecutive_failures=1 network_code=EAI_AGAIN",
      ]);
      expect(diagnostics.join(" ")).not.toContain("token");
      for (let index = 0; index < 4; index += 1) await sender.report("agent output");
      expect(attempts).toBe(4);
      expect(diagnostics).toHaveLength(2);
      expect(diagnostics[1]).toContain("consecutive_failures=5");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("resets the consecutive-failure counter after a successful report", async () => {
    let fail = true;
    const diagnostics: string[] = [];
    const sender = new BestEffortProgressReporter({
      progress: async () => {
        if (fail) throw new Error("offline");
      },
    }, (message) => diagnostics.push(message));

    await sender.report("first");
    fail = false;
    await sender.report("recovered");
    fail = true;
    await sender.report("failed again");

    expect(diagnostics).toHaveLength(2);
    expect(diagnostics.every((message) => message.includes("attempts=1"))).toBe(true);
  });
});
