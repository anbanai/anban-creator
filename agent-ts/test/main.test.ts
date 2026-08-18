import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough } from "node:stream";

import { uploadWorkspaceArtifacts } from "../src/artifacts.js";
import type { ResolvedBootstrapResponse } from "../src/bootstrap.js";
import {
  CompletionReportError,
  exitCodeForError,
  finalizationTimeouts,
  runJob,
  type RunJobDependencies,
} from "../src/main.js";
import type { ArtifactManifestFile, ExecutionResult, StageProgressEvent } from "../src/reporter.js";
import { buildQueryOptions } from "../src/runner.js";

describe("finalizationTimeouts", () => {
  test("uses independent defaults", () => {
    expect(finalizationTimeouts({})).toEqual({ artifact: 120_000, completion: 20_000 });
  });

  test("parses independent overrides and rejects invalid values", () => {
    expect(finalizationTimeouts({
      ANBAN_JOB_ARTIFACT_TIMEOUT: "45s",
      ANBAN_JOB_COMPLETION_TIMEOUT: "750ms",
    })).toEqual({ artifact: 45_000, completion: 750 });
    expect(finalizationTimeouts({
      ANBAN_JOB_ARTIFACT_TIMEOUT: "6m",
      ANBAN_JOB_COMPLETION_TIMEOUT: "1m30s",
    })).toEqual({ artifact: 120_000, completion: 20_000 });
    expect(finalizationTimeouts({ ANBAN_JOB_ARTIFACT_TIMEOUT: " 45s " }).artifact).toBe(45_000);
  });
});

describe("exitCodeForError", () => {
  test("reserves exit code two for an unacknowledged completion", () => {
    expect(exitCodeForError(new CompletionReportError(new Error("server unavailable")))).toBe(2);
    expect(exitCodeForError(new Error("bootstrap failed"))).toBe(1);
  });
});

const jobArgs = (workspace = "/workspace") => [
  "job", "--server-url", "https://creator.example.test",
  "--execution-id", "execution-1", "--workspace", workspace,
  "--workload-token-file", "/token",
];

const bootstrapData: ResolvedBootstrapResponse = {
  execution_token: "execution-token", task_id: "task-1", task_type: "article",
  agent_pack_id: "article", agent_pack_version: "1.0.0", agent_pack_digest: "0".repeat(64),
  runtime_profile: "article", runtime_adapter: "standard", project_id: "project-1", prompt: "write",
  execution_profile: {
    profile_id: "effective", provider: "deepseek", protocol: "anthropic", display_name: "effective",
    profile_fingerprint: "1".repeat(64), envs: {}, model_usage_aliases: {},
  },
  max_turns: 1, agent_flag: "anban:article", auto_memory_directory: ".claude/memory",
  files: [], artifact_transport: { mode: "direct" },
  resolved_agent_pack: {
    id: "article", version: "1.0.0", digest: "0".repeat(64), agent: { name: "article" },
    bindings: { task_types: ["article"] }, runtime: { profile: "article", adapter: "standard" },
    progress: [{
      id: "delivery", title: "Delivery", active_percent: 80, complete_percent: 100,
      required_artifacts: ["output/final.md"],
    }],
  },
};

function runJobHarness() {
  let shutdown = () => {};
  let completeCalls = 0;
  let artifactManifest: ArtifactManifestFile[] = [];
  let completed: ExecutionResult | undefined;
  const finalizationOrder: string[] = [];
  const stageProgressAttempts: StageProgressEvent[] = [];
  const stageProgressEvents: StageProgressEvent[] = [];
  let stageProgressImpl = async (_event: StageProgressEvent, _signal?: AbortSignal) => {};
  let completeImpl = async (_result: ExecutionResult, _signal?: AbortSignal) => {};
  const reporter = {
    progress: async () => {}, heartbeat: async () => {},
    stageProgress: async (event: StageProgressEvent, signal?: AbortSignal) => {
      stageProgressAttempts.push(event);
      await stageProgressImpl(event, signal);
      stageProgressEvents.push(event);
      finalizationOrder.push("progress");
    },
    prepareArtifactUpload: async (request: { relative_path: string }) => ({ upload_required: false, key: `existing/${request.relative_path}` }),
    streamArtifactContent: async () => ({ object_key: "existing", content_type: "text/plain", size: 0, sha256: "0".repeat(64) }),
    reportArtifactManifest: async (files: ArtifactManifestFile[]) => {
      artifactManifest = files;
      finalizationOrder.push("artifacts");
    },
    complete: async (result: ExecutionResult, signal?: AbortSignal) => {
      completeCalls += 1;
      completed = result;
      finalizationOrder.push("complete");
      await completeImpl(result, signal);
    },
  };
  const dependencies: RunJobDependencies = {
    readWorkloadToken: async () => "workload-token",
    bootstrap: async () => bootstrapData,
    materializeBootstrapFiles: async () => {},
    prepareWorkspace: async () => {},
    createReporter: () => reporter,
    startHeartbeat: () => () => {},
    runClaude: async () => ({ success: true, work_dir: "/workspace" }),
    uploadWorkspaceArtifacts: async () => 0,
    subscribeShutdown: (callback) => { shutdown = callback; return () => {}; },
  };
  return {
    dependencies,
    stdout: new PassThrough(), stderr: new PassThrough(),
    finalizationOrder,
    stageProgressAttempts,
    stageProgressEvents,
    artifactSignal: undefined as AbortSignal | undefined,
    completionSignal: undefined as AbortSignal | undefined,
    triggerShutdown: () => shutdown(),
    get artifactManifest() { return artifactManifest; },
    get completed() { return completed; },
    get completeCalls() { return completeCalls; },
    set stageProgress(value: typeof stageProgressImpl) { stageProgressImpl = value; },
    set complete(value: typeof completeImpl) { completeImpl = value; },
  };
}

async function invokeManagedStop(
  data: ResolvedBootstrapResponse,
  workspace: string,
  reporter: NonNullable<Parameters<typeof buildQueryOptions>[4]>,
  signal: AbortSignal,
) {
  const stop = buildQueryOptions(data, workspace, undefined, undefined, reporter).hooks!.Stop![0]!.hooks[0]!;
  return stop({
    session_id: "session-1",
    transcript_path: join(workspace, "transcript.jsonl"),
    cwd: workspace,
    hook_event_name: "Stop",
    stop_hook_active: false,
  }, undefined, { signal });
}

describe("runJob finalization", () => {
  test("emits final progress for a successful managed output", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-managed-finalization-"));
    const harness = runJobHarness();
    try {
      await mkdir(join(workspace, "output"));
      await writeFile(join(workspace, "output", "final.md"), "complete");
      harness.dependencies.runClaude = async (_config, data, reporter, signal) => {
        expect(await invokeManagedStop(data, workspace, reporter, signal)).toEqual({});
        return { success: true, work_dir: workspace };
      };
      harness.dependencies.uploadWorkspaceArtifacts = uploadWorkspaceArtifacts;

      const result = await runJob(jobArgs(workspace), harness.stdout, harness.stderr, harness.dependencies);

      expect(result).toEqual({ success: true, work_dir: workspace });
      expect(harness.stageProgressEvents).toEqual([
        expect.objectContaining({ stage: "delivery", state: "complete", progress_percent: 100 }),
      ]);
      expect(harness.artifactManifest.map((file) => file.relative_path)).toEqual(["output/final.md"]);
      expect(harness.completed).toEqual(result);
      expect(harness.finalizationOrder).toEqual(["progress", "artifacts", "complete"]);
    } finally {
      await rm(workspace, { recursive: true, force: true });
    }
  });

  test("continues artifact upload and completion when final progress delivery fails", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-managed-finalization-"));
    const harness = runJobHarness();
    try {
      await mkdir(join(workspace, "output"));
      await writeFile(join(workspace, "output", "final.md"), "complete");
      harness.stageProgress = async () => { throw new Error("progress server unavailable"); };
      harness.dependencies.runClaude = async (_config, data, reporter, signal) => {
        expect(await invokeManagedStop(data, workspace, reporter, signal)).toEqual({});
        return { success: true, work_dir: workspace };
      };
      harness.dependencies.uploadWorkspaceArtifacts = uploadWorkspaceArtifacts;

      const result = await runJob(jobArgs(workspace), harness.stdout, harness.stderr, harness.dependencies);

      expect(result).toEqual({ success: true, work_dir: workspace });
      expect(harness.stageProgressAttempts).toEqual([
        expect.objectContaining({ stage: "delivery", state: "complete", progress_percent: 100 }),
      ]);
      expect(harness.stageProgressEvents).toEqual([]);
      expect(harness.artifactManifest.map((file) => file.relative_path)).toEqual(["output/final.md"]);
      expect(harness.completed).toEqual(result);
      expect(harness.finalizationOrder).toEqual(["artifacts", "complete"]);
    } finally {
      await rm(workspace, { recursive: true, force: true });
    }
  });

  test("retains failure artifacts and never reports final progress for a failed managed output", async () => {
    const workspace = await mkdtemp(join(tmpdir(), "anban-managed-finalization-"));
    const harness = runJobHarness();
    const failedResult: ExecutionResult = {
      success: false,
      error: "delivery failed",
      terminal_reason: "provider_error",
      work_dir: workspace,
    };
    try {
      await mkdir(join(workspace, "output"));
      await writeFile(join(workspace, "output", "final.md"), "partial");
      await writeFile(join(workspace, "output", "failure-state.json"), JSON.stringify({ error: "delivery failed" }));
      harness.dependencies.runClaude = async (_config, data, reporter, signal) => {
        const stop = await invokeManagedStop(data, workspace, reporter, signal);
        expect(stop).toMatchObject({
          decision: "block",
          hookSpecificOutput: { additionalContext: expect.stringContaining("failure state output/failure-state.json") },
        });
        return failedResult;
      };
      harness.dependencies.uploadWorkspaceArtifacts = uploadWorkspaceArtifacts;

      const result = await runJob(jobArgs(workspace), harness.stdout, harness.stderr, harness.dependencies);

      expect(result).toEqual(failedResult);
      expect(harness.stageProgressAttempts).toEqual([]);
      expect(harness.artifactManifest.map((file) => file.relative_path)).toEqual([
        "output/failure-state.json",
        "output/final.md",
      ]);
      expect(harness.completed).toEqual(failedResult);
      expect(harness.finalizationOrder).toEqual(["artifacts", "complete"]);
    } finally {
      await rm(workspace, { recursive: true, force: true });
    }
  });

  test("reports a trusted pre-run workspace failure before exiting", async () => {
    const harness = runJobHarness();
    let completed: ExecutionResult | undefined;
    harness.dependencies.materializeBootstrapFiles = async () => { throw new Error("workspace rejected"); };
    harness.complete = async (result) => { completed = result; };

    await expect(runJob(jobArgs(), harness.stdout, harness.stderr, harness.dependencies)).rejects.toThrow("workspace rejected");
    expect(harness.completeCalls).toBe(1);
    expect(completed).toMatchObject({ success: false, terminal_reason: "platform_error", error: "workspace rejected" });
  });

  test("gives completion a fresh budget after artifact timeout", async () => {
    process.env.ANBAN_JOB_ARTIFACT_TIMEOUT = "10ms";
    process.env.ANBAN_JOB_COMPLETION_TIMEOUT = "100ms";
    const harness = runJobHarness();
    harness.dependencies.uploadWorkspaceArtifacts = async (_workspace, _data, _reporter, signal) => {
      harness.artifactSignal = signal;
      await new Promise<void>((_resolve, reject) => {
        signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
      });
      return 0;
    };
    harness.complete = async (_result, signal) => {
      harness.completionSignal = signal;
      expect(signal?.aborted).toBe(false);
      await Bun.sleep(30);
      expect(signal?.aborted).toBe(false);
    };

    const result = await runJob(jobArgs(), harness.stdout, harness.stderr, harness.dependencies);
    expect(result.success).toBe(false);
    expect(harness.completeCalls).toBe(1);
  });

  test("cancels artifact work on shutdown and still reports completion", async () => {
    const harness = runJobHarness();
    let completionStartedWithLiveSignal = false;
    harness.complete = async (_result, signal) => {
      harness.completionSignal = signal;
      completionStartedWithLiveSignal = signal?.aborted === false;
    };
    harness.dependencies.uploadWorkspaceArtifacts = async (_workspace, _data, _reporter, signal) => {
      harness.artifactSignal = signal;
      harness.triggerShutdown();
      await new Promise<void>((_resolve, reject) => {
        if (signal?.aborted) reject(signal.reason);
        else signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
      });
      return 0;
    };

    await runJob(jobArgs(), harness.stdout, harness.stderr, harness.dependencies);
    expect(harness.artifactSignal?.aborted).toBe(true);
    expect(completionStartedWithLiveSignal).toBe(true);
    expect(harness.completeCalls).toBe(1);
  });
});

afterEach(() => {
  delete process.env.ANBAN_JOB_ARTIFACT_TIMEOUT;
  delete process.env.ANBAN_JOB_COMPLETION_TIMEOUT;
});
