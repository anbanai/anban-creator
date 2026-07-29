import { pathToFileURL } from "node:url";

import { uploadWorkspaceArtifacts } from "./artifacts.js";
import { bootstrap, readWorkloadToken } from "./bootstrap.js";
import { parseJobConfig, type JobConfig } from "./config.js";
import { Reporter, type ExecutionResult } from "./reporter.js";
import { runClaude } from "./runner.js";
import { materializeBootstrapFiles, prepareWorkspace } from "./workspace.js";

const HEARTBEAT_INTERVAL_MS = 30_000;
const FINALIZATION_TIMEOUT_MS = 20_000;
const COMPLETION_RESERVE_MS = 5_000;

export function finalizationDeadlines(now: number, timeout = FINALIZATION_TIMEOUT_MS): { workDeadline: number; completionDeadline: number } {
  const reserve = Math.min(COMPLETION_RESERVE_MS, Math.floor(timeout / 2));
  return { workDeadline: now + timeout - reserve, completionDeadline: now + timeout };
}

export async function runJob(args: string[], stdout: NodeJS.WritableStream = process.stdout, stderr: NodeJS.WritableStream = process.stderr): Promise<ExecutionResult> {
  const config = parseJobConfig(args);
  const controller = new AbortController();
  const onSignal = () => controller.abort();
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);
  let stopHeartbeat: (() => void) | undefined;
  try {
    const workloadToken = await readWorkloadToken(config.workloadTokenFile);
    const data = await bootstrap(config, workloadToken, controller.signal);
    await materializeBootstrapFiles(config.workspace, data.files, controller.signal);
    await prepareWorkspace(config.workspace, data.task_type);
    const reporter = new Reporter(config, data.execution_token, data.task_id);
    stopHeartbeat = startHeartbeat(reporter, stderr, controller.signal);
    let result: ExecutionResult;
    try {
      result = await runClaude(config.workspace, data, config.serverURL, data.execution_token, reporter, controller.signal);
    } catch (error) {
      result = failure(config.workspace, error);
    }
    if (controller.signal.aborted && !result.success && !result.error) result.error = "agent shutdown: received termination signal";
    const deadlines = finalizationDeadlines(Date.now(), finalizationTimeout(config));
    const workAbort = abortAt(deadlines.workDeadline);
    try {
      await uploadWorkspaceArtifacts(config.workspace, data, reporter, workAbort.signal);
    } catch (error) {
      void reporter.progress(`artifact upload failed: ${(error as Error).message}`, workAbort.signal);
      if (result.success) result = failure(config.workspace, new Error(`artifact upload failed: ${(error as Error).message}`));
    } finally {
      workAbort.abort();
    }
    const completionAbort = abortAt(deadlines.completionDeadline);
    try { await reporter.complete(result, completionAbort.signal); } catch (error) { stderr.write(`failed to report completion: ${(error as Error).message}\n`); } finally { completionAbort.abort(); }
    stdout.write(`${JSON.stringify(result)}\n`);
    return result;
  } finally {
    stopHeartbeat?.();
    process.removeListener("SIGINT", onSignal);
    process.removeListener("SIGTERM", onSignal);
  }
}

function failure(workspace: string, error: unknown): ExecutionResult { return { success: false, error: error instanceof Error ? error.message : "agent execution failed", work_dir: workspace, terminal_reason: "platform_error" }; }

function startHeartbeat(reporter: Reporter, stderr: NodeJS.WritableStream, signal: AbortSignal): () => void {
  let failures = 0;
  const report = async (): Promise<void> => {
    try { await reporter.heartbeat(signal); failures = 0; }
    catch (error) { failures += 1; if (failures === 1 || failures % 5 === 0) stderr.write(`agent heartbeat failed (${failures} consecutive): ${(error as Error).message}\n`); }
  };
  void report();
  const interval = setInterval(() => void report(), HEARTBEAT_INTERVAL_MS);
  return () => clearInterval(interval);
}

function abortAt(deadline: number): AbortController {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), Math.max(0, deadline - Date.now()));
  controller.signal.addEventListener("abort", () => clearTimeout(timeout), { once: true });
  return controller;
}

function finalizationTimeout(_config: JobConfig): number {
  const value = process.env.ANBAN_JOB_FINALIZATION_TIMEOUT;
  if (!value) return FINALIZATION_TIMEOUT_MS;
  const match = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(value);
  if (!match) return FINALIZATION_TIMEOUT_MS;
  const unit = match[2] === "m" ? 60_000 : match[2] === "s" ? 1_000 : 1;
  const milliseconds = Number(match[1]) * unit;
  return milliseconds > 0 && milliseconds <= 300_000 ? milliseconds : FINALIZATION_TIMEOUT_MS;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runJob(process.argv.slice(2)).then((result) => { if (!result.success) process.exitCode = 1; }).catch((error) => { process.stderr.write(`${(error as Error).message}\n`); process.exitCode = 1; });
}
