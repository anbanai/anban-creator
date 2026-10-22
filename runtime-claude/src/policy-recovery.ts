import { createHash } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import { scanWorkspaceArtifacts } from "./artifacts.js";
import type { ResolvedBootstrapResponse } from "./bootstrap.js";
import type { ExecutionResult } from "./reporter.js";

export async function runWithProviderPolicyRecovery(
  workspace: string,
  data: ResolvedBootstrapResponse,
  runSession: (sessionData: ResolvedBootstrapResponse) => Promise<ExecutionResult>,
  reportProgress?: (message: string) => Promise<void>,
  signal?: AbortSignal,
): Promise<ExecutionResult> {
  let result = await runSession(data);
  if (!isProviderPolicyRejection(result) || signal?.aborted) return result;

  await reportProgress?.("provider policy rejection: starting one clean recovery session").catch(() => {});
  const recoveryData = await cleanRecoveryBootstrap(data, workspace, signal);
  const recovered = await runSession(recoveryData);
  result = recovered.success
    ? preserveProviderPolicyDiagnostic(recovered, result)
    : {
        ...recovered,
        recovery_attempted: true,
        provider_recovery: { status: "failed", initial_error_code: result.error_code, provider_code: result.provider_code },
      };
  return result;
}

function isProviderPolicyRejection(result: ExecutionResult): boolean {
  return result.success === false && (result.error_code === "provider_content_policy" || result.error_code === "provider_policy_rejection");
}

async function cleanRecoveryBootstrap(data: ResolvedBootstrapResponse, workspace: string, signal?: AbortSignal): Promise<ResolvedBootstrapResponse> {
  let paths: string[] = [];
  try {
    paths = (await scanWorkspaceArtifacts(workspace, signal)).map((artifact) => artifact.relativePath);
  } catch {
    // Recovery can still inspect the workspace directly when inventory fails.
  }
  const inventory = await recoveryInventory(workspace, paths.filter(isSafeRecoveryInventoryPath), signal);
  return {
    ...data,
    prompt: [
      "继续当前托管任务，但这是一个全新会话。不要复用或推测上一个会话的输入与输出。",
      `task_id=${data.task_id}`,
      `project_id=${data.project_id}`,
      `execution_id=${data.execution_id}`,
      "以下是运行时扫描到的文件候选清单，不代表文件有效、属于当前步骤或已通过质量审核：",
      "必须按当前 Agent Pack 和 Skill 合同逐项核验内容、来源、质量状态、计划数量和真实图片尺寸；仅在核验通过后复用文件，否则重新生成或写结构化失败态。不得仅因文件存在就报告完成。",
      inventory,
      "完成后确保 Agent Pack 要求的核心交付文件存在。",
    ].join("\n"),
    resume_session_id: undefined,
    resume_context_path: undefined,
    agent_memory_directory: undefined,
  };
}

async function recoveryInventory(workspace: string, paths: string[], signal?: AbortSignal): Promise<string> {
  const entries: string[] = [];
  for (const relativePath of paths.sort()) {
    signal?.throwIfAborted();
    try {
      const info = await lstat(`${workspace}/${relativePath}`);
      if (!info.isFile() || info.isSymbolicLink() || info.size > 25 * 1024 * 1024) continue;
      const digest = createHash("sha256").update(await readFile(`${workspace}/${relativePath}`)).digest("hex");
      entries.push(`- ${JSON.stringify(relativePath)} size=${info.size} sha256=${digest}`);
    } catch {
      // A concurrent or unsafe file is omitted from the recovery prompt.
    }
  }
  return entries.length > 0 ? entries.join("\n") : "- 当前未发现可安全列出的 output 文件";
}

function isSafeRecoveryInventoryPath(path: string): boolean {
  return /^output\/[\p{L}\p{N}][\p{L}\p{N}._/-]{0,511}$/u.test(path)
    && !path.includes("//")
    && path.split("/").every((part) => part !== "." && part !== "..");
}

function preserveProviderPolicyDiagnostic(recovered: ExecutionResult, rejected: ExecutionResult): ExecutionResult {
  return {
    ...recovered,
    error_code: rejected.error_code,
    policy_domain: rejected.policy_domain,
    provider_code: rejected.provider_code,
    http_status: rejected.http_status,
    content_direction: rejected.content_direction ?? "unknown",
    recoverable: rejected.recoverable,
    request_id: rejected.request_id,
    failure_stage: rejected.failure_stage,
    resume_from: rejected.resume_from,
    recovery_attempted: true,
    recovery_succeeded: true,
    provider_recovery: {
      status: "succeeded",
      initial_error_code: rejected.error_code,
      provider_code: rejected.provider_code,
      request_id: rejected.request_id,
    },
  };
}
