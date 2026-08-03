import { afterEach, describe, expect, test } from "bun:test";
import { PassThrough } from "node:stream";

import type { BootstrapResponse } from "../src/bootstrap.js";
import type { ExecutionResult } from "../src/reporter.js";
import {
  CompletionReportError,
  exitCodeForError,
  finalizationTimeouts,
  runJob,
  type RunJobDependencies,
} from "../src/main.js";

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

const jobArgs = [
  "job", "--server-url", "https://creator.example.test",
  "--execution-id", "execution-1", "--workspace", "/workspace",
  "--workload-token-file", "/token",
];

const bootstrapData: BootstrapResponse = {
  execution_token: "execution-token", task_id: "task-1", task_type: "article",
  agent_pack_id: "article", agent_pack_version: "1.0.0", agent_pack_digest: "0".repeat(64),
  runtime_profile: "article", runtime_adapter: "standard", project_id: "project-1", prompt: "write",
  execution_profile: {
    profile_id: "effective", provider: "deepseek", protocol: "anthropic", display_name: "effective",
    profile_fingerprint: "1".repeat(64), envs: {}, model_usage_aliases: {},
  },
  max_turns: 1, agent_flag: "anban:article", auto_memory_directory: ".claude/memory",
  files: [], artifact_transport: { mode: "direct" },
};

function runJobHarness() {
  let shutdown = () => {};
  let completeCalls = 0;
  let completeImpl = async (_result: ExecutionResult, _signal?: AbortSignal) => {};
  const reporter = {
    progress: async () => {}, heartbeat: async () => {},
    prepareArtifactUpload: async () => ({ upload_required: false, key: "existing" }),
    streamArtifactContent: async () => ({ object_key: "existing", content_type: "text/plain", size: 0, sha256: "0".repeat(64) }),
    reportArtifactManifest: async () => {},
    complete: async (result: ExecutionResult, signal?: AbortSignal) => {
      completeCalls += 1;
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
    artifactSignal: undefined as AbortSignal | undefined,
    completionSignal: undefined as AbortSignal | undefined,
    triggerShutdown: () => shutdown(),
    get completeCalls() { return completeCalls; },
    set complete(value: typeof completeImpl) { completeImpl = value; },
  };
}

describe("runJob finalization", () => {
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

    const result = await runJob(jobArgs, harness.stdout, harness.stderr, harness.dependencies);
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

    await runJob(jobArgs, harness.stdout, harness.stderr, harness.dependencies);
    expect(harness.artifactSignal?.aborted).toBe(true);
    expect(completionStartedWithLiveSignal).toBe(true);
    expect(harness.completeCalls).toBe(1);
  });
});

afterEach(() => {
  delete process.env.ANBAN_JOB_ARTIFACT_TIMEOUT;
  delete process.env.ANBAN_JOB_COMPLETION_TIMEOUT;
});
