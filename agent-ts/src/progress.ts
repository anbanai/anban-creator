import type { StageProgressEvent } from "./reporter.js";

export type ProgressEvent = { stage: string; state: "active" | "complete"; description?: string };

export interface ProgressHandleResult {
  emitted: boolean;
}

export interface StageProgressReporter {
  stageProgress(event: StageProgressEvent, signal?: AbortSignal): Promise<void>;
}

export type ProgressDiagnostic = (message: string) => void;

export class ProgressEmitter {
  private lastEmittedKey?: string;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly reporter: StageProgressReporter,
    private readonly diagnostic: ProgressDiagnostic = (message) => console.error(message),
  ) {}

  handle(event: ProgressEvent, signal?: AbortSignal): Promise<ProgressHandleResult> {
    const operation = () => this.emit(event, signal);
    const result = this.queue.then(operation, operation);
    this.queue = result.then(() => {}, () => {});
    return result;
  }

  private async emit(event: ProgressEvent, signal?: AbortSignal): Promise<ProgressHandleResult> {
    try {
      return await this.emitOrdered(event, signal);
    } catch (error) {
      this.diagnostic(`progress emitter failed: ${error instanceof Error ? error.message : String(error)}`);
      return { emitted: false };
    }
  }

  private async emitOrdered(event: ProgressEvent, signal?: AbortSignal): Promise<ProgressHandleResult> {
    const dedupeKey = JSON.stringify([event.stage, event.state, event.description ?? ""]);
    if (this.lastEmittedKey === dedupeKey) return { emitted: false };

    const progressEvent: StageProgressEvent = {
      stage: event.stage,
      state: event.state,
      description: event.description,
    };
    try {
      if (signal) await this.reporter.stageProgress(progressEvent, signal);
      else await this.reporter.stageProgress(progressEvent);
      this.lastEmittedKey = dedupeKey;
      return { emitted: true };
    } catch (error) {
      this.diagnostic(`progress reporter failed: ${error instanceof Error ? error.message : String(error)}`);
      return { emitted: false };
    }
  }
}
