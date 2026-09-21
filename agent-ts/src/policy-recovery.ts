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
    : { ...recovered, recovery_attempted: true };
  return result;
}

function isProviderPolicyRejection(result: ExecutionResult): boolean {
  return result.success === false && result.error_code === "provider_policy_rejection";
}

async function cleanRecoveryBootstrap(data: ResolvedBootstrapResponse, workspace: string, signal?: AbortSignal): Promise<ResolvedBootstrapResponse> {
  let paths: string[] = [];
  try {
    paths = (await scanWorkspaceArtifacts(workspace, signal)).map((artifact) => artifact.relativePath);
  } catch {
    // Recovery can still inspect the workspace directly when inventory fails.
  }
  const safePaths = paths.filter(isSafeRecoveryInventoryPath);
  const inventory = safePaths.length > 0
    ? safePaths.map((path) => `- ${JSON.stringify(path)}`).join("\n")
    : "- 当前未发现可安全列出的 output 文件";
  return {
    ...data,
    prompt: [
      "继续当前托管任务，但这是一个全新会话。不要复用或推测上一个会话的输入与输出。",
      `task_id=${data.task_id}`,
      `project_id=${data.project_id}`,
      `execution_id=${data.execution_id}`,
      "优先检查并复用以下已有文件，只补齐缺失步骤；不得重新生成已经存在且有效的图片：",
      inventory,
      "完成后确保 Agent Pack 要求的核心交付文件存在。",
    ].join("\n"),
    resume_session_id: undefined,
    resume_context_path: undefined,
    agent_memory_directory: undefined,
  };
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
  };
}
