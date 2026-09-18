import { pathToFileURL } from "node:url";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";

import { uploadWorkspaceArtifacts, type ArtifactReporter, type ArtifactUploadSummary } from "./artifacts.js";
import { BootstrapResponseError, bootstrap, readWorkloadToken, type BootstrapIdentity, type BootstrapResponse, type ResolvedBootstrapResponse } from "./bootstrap.js";
import { parseJobConfig, type JobConfig } from "./config.js";
import { CompletionReportError, exitCodeForError } from "./errors.js";
import { Reporter, type ExecutionResult } from "./reporter.js";
import { runWithProviderPolicyRecovery } from "./policy-recovery.js";
import { runClaude, type RunnerReporter } from "./runner.js";
import { materializeBootstrapFiles, prepareWorkspace } from "./workspace.js";

const HEARTBEAT_INTERVAL_MS = 30_000;
const ARTIFACT_TIMEOUT_MS = 120_000;
const COMPLETION_TIMEOUT_MS = 20_000;
const MAX_ARTIFACT_TIMEOUT_MS = 300_000;
const MAX_COMPLETION_TIMEOUT_MS = 60_000;
const WORKFLOW_FAILURE_MESSAGE = "任务执行未完成，已有产物已保留。";
const EXECUTION_IDENTITY_FAILURE_MESSAGE = "执行环境未建立，暂时无法生成或结算图片。";

type HeartbeatReporter = Pick<Reporter, "heartbeat">;
type JobReporter = ArtifactReporter & RunnerReporter & HeartbeatReporter & Pick<Reporter, "complete" | "submitCompletionMetadata">;

export interface FinalizationTimeouts {
  artifact: number;
  completion: number;
}

export interface RunJobDependencies {
  readWorkloadToken(path: string): Promise<string>;
  bootstrap(config: JobConfig, token: string, signal?: AbortSignal): Promise<ResolvedBootstrapResponse>;
  materializeBootstrapFiles(workspace: string, files: BootstrapResponse["files"], signal?: AbortSignal): Promise<void>;
  prepareWorkspace(workspace: string, taskType: string, adapter: BootstrapResponse["runtime_adapter"]): Promise<void>;
  createReporter(config: JobConfig, data: Pick<BootstrapResponse, "execution_token" | "execution_id" | "task_id">): JobReporter;
  startHeartbeat(reporter: JobReporter, stderr: NodeJS.WritableStream, signal: AbortSignal): () => void;
  runClaude(config: JobConfig, data: ResolvedBootstrapResponse, reporter: JobReporter, signal: AbortSignal): Promise<ExecutionResult>;
  uploadWorkspaceArtifacts(workspace: string, data: BootstrapResponse, reporter: JobReporter, signal?: AbortSignal): Promise<ArtifactUploadSummary>;
  subscribeShutdown(onSignal: () => void): () => void;
}

export { CompletionReportError, exitCodeForError } from "./errors.js";

export function finalizationTimeouts(env: NodeJS.ProcessEnv = process.env): FinalizationTimeouts {
  return {
    artifact: parsePhaseTimeout(env.ANBAN_JOB_ARTIFACT_TIMEOUT, ARTIFACT_TIMEOUT_MS, MAX_ARTIFACT_TIMEOUT_MS),
    completion: parsePhaseTimeout(env.ANBAN_JOB_COMPLETION_TIMEOUT, COMPLETION_TIMEOUT_MS, MAX_COMPLETION_TIMEOUT_MS),
  };
}

function parsePhaseTimeout(raw: string | undefined, fallback: number, maximum: number): number {
  if (!raw) return fallback;
  const match = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(raw.trim());
  if (!match) return fallback;
  const unit = match[2] === "m" ? 60_000 : match[2] === "s" ? 1_000 : 1;
  const milliseconds = Number(match[1]) * unit;
  return milliseconds > 0 && milliseconds <= maximum ? milliseconds : fallback;
}

const defaultRunJobDependencies: RunJobDependencies = {
  readWorkloadToken,
  bootstrap,
  materializeBootstrapFiles,
  prepareWorkspace,
  createReporter: (config, data) => new Reporter({ ...config, executionID: data.execution_id }, data.execution_token, data.task_id),
  startHeartbeat,
  runClaude: (config, data, reporter, signal) =>
    runClaude(config.workspace, data, config.serverURL, data.execution_token, reporter, signal),
  uploadWorkspaceArtifacts,
  subscribeShutdown: (onSignal) => {
    process.once("SIGINT", onSignal);
    process.once("SIGTERM", onSignal);
    return () => {
      process.removeListener("SIGINT", onSignal);
      process.removeListener("SIGTERM", onSignal);
    };
  },
};

export async function runJob(
  args: string[],
  stdout: NodeJS.WritableStream = process.stdout,
  stderr: NodeJS.WritableStream = process.stderr,
  dependencies: RunJobDependencies = defaultRunJobDependencies,
): Promise<ExecutionResult> {
  const config = parseJobConfig(args);
  const shutdown = new AbortController();
  const stopShutdown = dependencies.subscribeShutdown(() => shutdown.abort(new Error("agent shutdown: received termination signal")));
  let stopHeartbeat: (() => void) | undefined;
  try {
    const workloadToken = await dependencies.readWorkloadToken(config.workloadTokenFile);
    let data: ResolvedBootstrapResponse | undefined;
    try {
      data = await dependencies.bootstrap(config, workloadToken, shutdown.signal);
      await dependencies.materializeBootstrapFiles(config.workspace, data.files, shutdown.signal);
      await dependencies.prepareWorkspace(config.workspace, data.task_type, data.runtime_adapter);
    } catch (error) {
      const identity: BootstrapIdentity | undefined = data ?? (error instanceof BootstrapResponseError ? error.identity : undefined);
      if (identity) {
        const reporter = dependencies.createReporter(config, identity);
        const completionAbort = abortAfter(finalizationTimeouts().completion);
        try {
          await reporter.complete(failure(config.workspace, error), completionAbort.signal);
        } catch (completeError) {
          throw new CompletionReportError(completeError instanceof Error ? completeError : new Error("completion report failed"));
        } finally {
          completionAbort.abort();
        }
      }
      throw error;
    }
    const reporter = dependencies.createReporter(config, data);
    stopHeartbeat = dependencies.startHeartbeat(reporter, stderr, shutdown.signal);
    let result: ExecutionResult;
    result = await runWithProviderPolicyRecovery(
      config.workspace,
      data,
      async (sessionData) => {
        try {
          return await dependencies.runClaude(config, sessionData, reporter, shutdown.signal);
        } catch (error) {
          return failure(config.workspace, error);
        }
      },
      (message) => reporter.progress(message, shutdown.signal),
      shutdown.signal,
    );
    result = await applyFailureState(config.workspace, result);
    if (shutdown.signal.aborted && !result.success && !result.error) result.error = "agent shutdown: received termination signal";

    const timeouts = finalizationTimeouts();
    const artifactStartedAt = Date.now();
    const artifactAbort = abortAfter(timeouts.artifact, shutdown.signal);
    try {
      stderr.write(`artifact finalization started: timeout_ms=${timeouts.artifact}\n`);
      const summary = await dependencies.uploadWorkspaceArtifacts(config.workspace, data, reporter, artifactAbort.signal);
      if (summary.failures.length > 0) result = { ...result, artifact_upload_failures: summary.failures };
      stderr.write(`artifact finalization completed: files=${summary.uploaded} failures=${summary.failures.length} duration_ms=${Date.now() - artifactStartedAt}\n`);
    } catch (error) {
      const message = error instanceof Error ? error.message : "artifact finalization failed";
      stderr.write(`artifact finalization failed: duration_ms=${Date.now() - artifactStartedAt} error=${message}\n`);
      void reporter.progress(`artifact upload failed: ${message}`, artifactAbort.signal).catch(() => {});
      if (result.success) result = failure(config.workspace, new Error(`artifact upload failed: ${message}`));
    } finally {
      artifactAbort.abort();
    }

    const completionStartedAt = Date.now();
    const completionAbort = abortAfter(timeouts.completion);
    let completionError: Error | undefined;
    try {
      stderr.write(`completion report started: timeout_ms=${timeouts.completion}\n`);
      try {
        const metadata = JSON.parse(await readFile(`${config.workspace}/output/completion-metadata.json`, "utf8"));
        await reporter.submitCompletionMetadata(metadata, completionAbort.signal);
      } catch (metadataError) {
        stderr.write(`completion metadata upload skipped: ${(metadataError as Error).message}\n`);
      }
      await reporter.complete(result, completionAbort.signal);
      stderr.write(`completion report acknowledged: duration_ms=${Date.now() - completionStartedAt}\n`);
    } catch (error) {
      completionError = error instanceof Error ? error : new Error("completion report failed");
      stderr.write(`completion report exhausted: duration_ms=${Date.now() - completionStartedAt} error=${completionError.message}\n`);
    } finally {
      completionAbort.abort();
    }
    stdout.write(`${JSON.stringify(result)}\n`);
    if (completionError) throw new CompletionReportError(completionError, result);
    return result;
  } finally {
    stopHeartbeat?.();
    stopShutdown();
  }
}

function failure(workspace: string, error: unknown): ExecutionResult {
  return {
    success: false,
    error: error instanceof Error ? error.message : "agent execution failed",
    work_dir: workspace,
    terminal_reason: "platform_error",
  };
}

async function applyFailureState(workspace: string, result: ExecutionResult): Promise<ExecutionResult> {
  const path = `${workspace}/output/failure-state.json`;
  let file: Awaited<ReturnType<typeof open>> | undefined;
  try {
    file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const info = await file.stat();
    if (!info.isFile() || info.size > 64 * 1024) return result;
    const value = JSON.parse(await file.readFile("utf8")) as Record<string, unknown>;
    const code = typeof value.error_code === "string" && /^[a-z0-9_]{1,64}$/.test(value.error_code) ? value.error_code : undefined;
    const stage = typeof value.stage === "string" && /^[a-z0-9_]{1,64}$/.test(value.stage) ? value.stage : undefined;
    const resumeFrom = typeof value.resume_from === "string" && /^[a-z0-9_]{1,64}$/.test(value.resume_from) ? value.resume_from : undefined;
    if (value.version !== "1.0" || value.status !== "recoverable_failure" || !code || !stage || !resumeFrom) return result;
    if (typeof value.message !== "string" || !value.message.trim()) return result;
    const trustedIdentityFailure = result.success === false && result.terminal_reason === "platform_error" && result.root_error_code === "execution_identity_unavailable";
    if (trustedIdentityFailure && code !== "execution_identity_unavailable") return result;
    return {
      ...result,
      success: false,
      error: trustedIdentityFailure ? EXECUTION_IDENTITY_FAILURE_MESSAGE : WORKFLOW_FAILURE_MESSAGE,
      terminal_reason: trustedIdentityFailure ? "platform_error" : (result.success ? "workflow_error" : result.terminal_reason ?? "workflow_error"),
      ...(trustedIdentityFailure
        ? { root_error_code: "execution_identity_unavailable" }
        : { workflow_error_code: code }),
      failure_stage: stage,
      resume_from: resumeFrom,
    };
  } catch {
    return result;
  } finally {
    await file?.close().catch(() => {});
  }
}

function startHeartbeat(reporter: HeartbeatReporter, stderr: NodeJS.WritableStream, signal: AbortSignal): () => void {
  let failures = 0;
  const report = async (): Promise<void> => {
    try { await reporter.heartbeat(signal); failures = 0; }
    catch (error) { failures += 1; if (failures === 1 || failures % 5 === 0) stderr.write(`agent heartbeat failed (${failures} consecutive): ${(error as Error).message}\n`); }
  };
  void report();
  const interval = setInterval(() => void report(), HEARTBEAT_INTERVAL_MS);
  return () => clearInterval(interval);
}

function abortAfter(timeout: number, parent?: AbortSignal): AbortController {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(new Error("phase deadline exceeded")), timeout);
  const abortFromParent = () => controller.abort(parent?.reason);
  if (parent?.aborted) abortFromParent();
  else parent?.addEventListener("abort", abortFromParent, { once: true });
  controller.signal.addEventListener("abort", () => {
    clearTimeout(timer);
    parent?.removeEventListener("abort", abortFromParent);
  }, { once: true });
  return controller;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const args = process.argv.slice(2);
  const execute = runJob(args);
  execute
    .then((result) => { if (!result.success) process.exitCode = 1; })
    .catch((error) => {
      process.stderr.write(`${error instanceof Error ? error.message : "agent job failed"}\n`);
      process.exitCode = exitCodeForError(error);
    });
}
