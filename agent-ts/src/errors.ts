export class CompletionReportError extends Error {
  constructor(cause: Error) {
    super(`failed to report completion: ${cause.message}`, { cause });
    this.name = "CompletionReportError";
  }
}

export function exitCodeForError(error: unknown): number {
  return error instanceof CompletionReportError ? 2 : 1;
}
