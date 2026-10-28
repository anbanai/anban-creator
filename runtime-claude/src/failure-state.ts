import { constants } from "node:fs";
import { lstat, open } from "node:fs/promises";
import { resolve } from "node:path";

import { validateRequiredArtifactInternal } from "./artifact-validator-internal.js";

const FAILURE_STATE_PATH = "output/failure-state.json";
const MAX_FAILURE_STATE_BYTES = 64 * 1024;

export interface WorkflowFailureState {
  error_code: string;
  stage: string;
  resume_from: string;
}

// Workspace evidence may end a failed workflow, but never establishes a
// trusted platform failure or permits a successful delivery.
export async function readWorkflowFailureState(workspace: string): Promise<WorkflowFailureState | undefined> {
  let file: Awaited<ReturnType<typeof open>> | undefined;
  try {
    if (!(await lstat(resolve(workspace, "output"))).isDirectory()) return undefined;
    if (!await validateRequiredArtifactInternal(workspace, FAILURE_STATE_PATH)) return undefined;
    file = await open(resolve(workspace, FAILURE_STATE_PATH), constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    const info = await file.stat();
    if (!info.isFile() || info.size > MAX_FAILURE_STATE_BYTES) return undefined;
    const bytes = Buffer.alloc(MAX_FAILURE_STATE_BYTES + 1);
    const { bytesRead } = await file.read(bytes, 0, bytes.length, 0);
    if (bytesRead > MAX_FAILURE_STATE_BYTES) return undefined;
    const value: unknown = JSON.parse(bytes.subarray(0, bytesRead).toString("utf8"));
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const state = value as Record<string, unknown>;
    if (state.version !== "1.0" || state.status !== "recoverable_failure"
      || typeof state.message !== "string" || !state.message.trim()) return undefined;
    const { error_code, stage, resume_from } = state;
    if (typeof error_code !== "string" || !/^[a-z0-9_]{1,64}$/.test(error_code)
      || typeof stage !== "string" || !/^[a-z0-9_]{1,64}$/.test(stage)
      || typeof resume_from !== "string" || !/^[a-z0-9_]{1,64}$/.test(resume_from)) return undefined;
    return { error_code, stage, resume_from };
  } catch {
    return undefined;
  } finally {
    await file?.close().catch(() => {});
  }
}
