export class CompletionReportError extends Error {
  readonly code = "completion_report_failed";
  constructor(cause: Error, readonly rootError?: unknown) {
    super(`failed to report completion: ${cause.message}`, { cause });
    this.name = "CompletionReportError";
  }
}

export function exitCodeForError(error: unknown): number {
  if (!(error instanceof CompletionReportError)) return 1;
  const root = error.rootError as { success?: unknown; terminal_reason?: unknown; root_error_code?: unknown } | undefined;
  if (root?.success === false && root.terminal_reason === "platform_error" && root.root_error_code === "execution_identity_unavailable") return 3;
  return 2;
}
