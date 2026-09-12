export class CompletionReportError extends Error {
  readonly code = "completion_report_failed";
  constructor(cause: Error, readonly rootError?: unknown) {
    super(`failed to report completion: ${cause.message}`, { cause });
    this.name = "CompletionReportError";
  }
}

export class ExecutionIdentityError extends Error {
  readonly code = "execution_identity_unavailable";
  readonly resumeFrom = "image_generation";
  constructor(message = "执行环境未建立，暂时无法生成或结算图片") {
    super(message);
    this.name = "ExecutionIdentityError";
  }
}

export function exitCodeForError(error: unknown): number {
  return error instanceof CompletionReportError ? 2 : 1;
}
