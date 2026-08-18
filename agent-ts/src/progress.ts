import { validateAllStageArtifacts, validateStageArtifacts, type ArtifactValidationResult } from "./artifact-validator.js";
import type { AgentPack, AgentPackProgressStage } from "./bootstrap.js";
import type { StageProgressEvent } from "./reporter.js";

export type ProgressState = "active" | "complete" | "final";

export type ProgressEvent =
  | { stage: string; state: "active" | "complete"; description?: string }
  | { state: "final"; description?: string };

export interface ProgressHandleResult {
  emitted: boolean;
  validation?: ArtifactValidationResult;
}

export interface StageProgressReporter {
  stageProgress(event: StageProgressEvent, signal?: AbortSignal): Promise<void>;
}

export type ProgressDiagnostic = (message: string) => void;

export class ProgressEmitter {
  private readonly emitted = new Set<string>();
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly pack: AgentPack,
    private readonly reporter: StageProgressReporter,
    private readonly diagnostic: ProgressDiagnostic = (message) => console.error(message),
  ) {}

  handle(event: ProgressEvent, workspace: string, signal?: AbortSignal): Promise<ProgressHandleResult> {
    const operation = () => this.emit(event, workspace, signal);
    const result = this.queue.then(operation, operation);
    this.queue = result.then(() => {}, () => {});
    return result;
  }

  private async emit(event: ProgressEvent, workspace: string, signal?: AbortSignal): Promise<ProgressHandleResult> {
    try {
      return await this.emitValidated(event, workspace, signal);
    } catch (error) {
      this.diagnostic(`progress emitter failed: ${error instanceof Error ? error.message : String(error)}`);
      return { emitted: false };
    }
  }

  private async emitValidated(event: ProgressEvent, workspace: string, signal?: AbortSignal): Promise<ProgressHandleResult> {
    const stage = event.state === "final"
      ? this.pack.progress.at(-1)
      : this.pack.progress.find((candidate) => candidate.id === event.stage);
    if (!stage) {
      this.diagnostic(event.state === "final"
        ? "progress final event has no declared stage"
        : `unknown progress stage: ${event.stage}`);
      return { emitted: false };
    }

    const reporterState = event.state === "active" ? "active" : "complete";
    const dedupeKey = `${stage.id}:${reporterState}`;
    if (this.emitted.has(dedupeKey)) return { emitted: false };

    let validation: ArtifactValidationResult | undefined;
    if (event.state === "complete") {
      validation = await validateStageArtifacts(this.pack, stage.id, workspace);
    } else if (event.state === "final") {
      validation = await validateAllStageArtifacts(this.pack, workspace);
    }
    if (validation && !validation.ok) return { emitted: false, validation };
    if (event.state === "complete" && stage === this.pack.progress.at(-1)) {
      return { emitted: false, validation: validation ?? { ok: true } };
    }

    const progressEvent: StageProgressEvent = {
      stage: stage.id,
      state: reporterState,
      title: stage.title,
      description: event.description,
      progress_percent: progressPercent(stage, event.state),
    };
    try {
      if (signal) await this.reporter.stageProgress(progressEvent, signal);
      else await this.reporter.stageProgress(progressEvent);
      this.emitted.add(dedupeKey);
      return validation ? { emitted: true, validation } : { emitted: true };
    } catch (error) {
      this.diagnostic(`progress reporter failed: ${error instanceof Error ? error.message : String(error)}`);
      return validation ? { emitted: false, validation } : { emitted: false };
    }
  }
}

function progressPercent(stage: AgentPackProgressStage, state: ProgressState): number {
  if (state === "active") return stage.active_percent;
  if (state === "complete") return stage.complete_percent;
  return 100;
}
