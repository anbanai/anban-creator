import { http, unwrap } from '@/lib/http-client'
import type { InteractiveCommitResult } from './interactive'
import type { VideoEditDefaults } from '@/types/video-edit'
import type { InputAttachment } from '@/types/input-attachment'

const ASSISTANT_COMMAND_TIMEOUT_MS = 15 * 60 * 1000

export type AssistantModeID = 'profile' | 'creation' | 'review'

export type AssistantInteractionLifecycle = 'active' | 'reconnecting' | 'needs_confirmation' | 'completed' | 'abandoned' | string

export interface AssistantInteractionDraftReceipt {
  draft_id: string
  mode_id: AssistantModeID
  revision: number
  payload: Record<string, unknown>
  payload_digest: string
  ready: boolean
  confirmation_state: 'pending' | 'confirmed' | 'consumed' | 'superseded' | string
}

export interface AssistantInteractionSnapshot {
  interaction_id: string
  mode_id: AssistantModeID
  project_id?: string
  content_id?: string
  lifecycle: AssistantInteractionLifecycle
  generation: number
  runtime_status: string
  expires_at: string
  last_activity_at: string
  available_actions: string[]
  recovery_error_code?: string
  drafts: AssistantInteractionDraftReceipt[]
  commit_result?: AssistantInteractionCommitResult
}

export interface AssistantInteractionCommitRequest {
  draft_id: string
  revision: number
  idempotency_key: string
}

export interface AssistantInteractionDraftUpdateRequest {
  generation: number
  revision: number
  attempt_id: string
  payload: Record<string, unknown>
}

export interface AssistantInteractionRecoveryContext {
  version: 1
  interaction_id: string
  mode_id: AssistantModeID
  messages: Array<{ role: 'user' | 'assistant'; text: string }>
  questions: Array<{ question: string; answers: string[] }>
}

export interface AssistantInteractionCommitResult {
  status: 'created' | 'already_created' | 'needs_reconciliation' | string
  task_id?: string
  result?: Record<string, unknown>
}

export interface AssistantMode {
  id: AssistantModeID
  displayName: string
  description: string
  available: boolean
  disabledReason?: string
}

export interface AssistantWork {
  id: string
  title: string
  type: string
  createdAt: string
}

export interface AssistantProject {
  id: string
  name: string
  platform: string
  profileRevision: number
  profileReady: boolean
  avatar_url?: string
  description?: string
  image_ratio?: string
  portrait_reference_image?: {
    download_url?: string
    preview_url?: string
  } | null
  video_edit_defaults?: VideoEditDefaults
  works: AssistantWork[]
}

export interface AssistantWorkPage {
  items: AssistantWork[]
  next_cursor?: { created_at: string; id: string }
  has_more: boolean
}

export interface AssistantBootstrap {
  modes: AssistantMode[]
  defaultModeId: AssistantModeID
  projects: AssistantProject[]
}

export interface AssistantCheckpointRequest {
  schema_version: string
  payload: Record<string, unknown>
  summary: string
  status: 'draft' | 'ready'
}

export interface AssistantWorkspace {
  id: string
  mode_id: AssistantModeID
  project_id?: string
  content_id?: string
  runtime_id?: string
  runtime_status: string
  expires_at: string
  generation?: number
  interaction_snapshot?: AssistantInteractionSnapshot
}

export interface AssistantWorkspaceDraft {
  id: string
  payload_digest: string
  mode_id: AssistantModeID
  project_id?: string
  content_id?: string
  expires_at: string
  payload: Record<string, unknown>
  revision?: number
}

export const assistantApi = {
  bootstrap: () => unwrap<AssistantBootstrap>(http.get('/assistant/bootstrap')),

  works: (projectId: string, cursor?: { created_at: string; id: string }, limit = 25) =>
    unwrap<AssistantWorkPage>(
      http.get(`/assistant/projects/${encodeURIComponent(projectId)}/works`, {
        params: {
          ...(cursor ? { before_created_at: cursor.created_at, before_id: cursor.id } : {}),
          limit,
        },
      }),
    ),

  createWorkspace: async (
    request: {
      mode_id: AssistantModeID
      project_id?: string
      content_id?: string
    },
    idempotencyKey?: string,
  ) =>
    interactionSnapshotAsWorkspace(
      await assistantApi.createInteraction(
        {
          mode_id: request.mode_id,
          project_id: request.project_id,
          content_id: request.content_id,
        },
        idempotencyKey,
      ),
    ),

  switchWorkspaceMode: (
    workspaceId: string,
    modeId: AssistantModeID,
    context?: {
      project_id?: string
      content_id?: string
      recovery_context?: AssistantInteractionRecoveryContext
    },
  ) =>
    assistantApi
      .createInteraction({
        mode_id: modeId,
        project_id: context?.project_id,
        content_id: context?.content_id,
        resume_interaction_id: workspaceId,
        recovery_context: context?.recovery_context,
      })
      .then(interactionSnapshotAsWorkspace),

  workspaceMessage: async (workspaceId: string, modeId: AssistantModeID, text: string, clientMessageId: string = crypto.randomUUID(), attachments: InputAttachment[] = []) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    return assistantApi.sendInteractionMessage(workspaceId, {
      generation: snapshot.generation,
      mode_id: modeId,
      client_message_id: clientMessageId,
      text,
      attachments: attachments.flatMap((attachment) => attachment.asset_id ? [{ asset_id: attachment.asset_id }] : []),
    })
  },

  workspaceAnswer: async (
    workspaceId: string,
    modeId: AssistantModeID,
    callId: string,
    answers: Array<{ id: string; selected: string[]; custom?: string }>,
    attemptId: string = crypto.randomUUID(),
    generation?: number,
  ) => {
    const currentGeneration = generation ?? (await assistantApi.getInteractionSnapshot(workspaceId)).generation
    return assistantApi.answerInteractionQuestion(workspaceId, callId, {
      generation: currentGeneration,
      mode_id: modeId,
      attempt_id: attemptId,
      answers,
    })
  },

  workspaceCheckpoint: async (workspaceId: string, modeId: AssistantModeID, _checkpoint: AssistantCheckpointRequest) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    const draft = [...snapshot.drafts].filter((item) => item.mode_id === modeId).sort((a, b) => b.revision - a.revision)[0]
    if (!draft) throw new Error('当前计划尚未保存，请重试暂存')
    return {
      id: draft.draft_id,
      payload_digest: draft.payload_digest,
      revision: draft.revision,
      payload: draft.payload,
      mode_id: modeId,
      project_id: snapshot.project_id,
      content_id: snapshot.content_id,
      expires_at: snapshot.expires_at,
    } satisfies AssistantWorkspaceDraft
  },

  workspaceCommit: async (workspaceId: string, _modeId: AssistantModeID, payload: AssistantInteractionCommitRequest) => {
    const result = await assistantApi.commitInteraction(workspaceId, payload)
    if (result.result && typeof result.result === 'object')
      return {
        ...(result.result as InteractiveCommitResult & Record<string, unknown>),
        interaction_status: result.status,
        task_id: result.task_id,
      } as InteractiveCommitResult & Record<string, unknown>
    return {
      interaction_status: result.status,
      task_id: result.task_id,
      task: result.task_id ? { id: result.task_id } : undefined,
    }
  },

  closeWorkspace: (workspaceId: string) => http.delete(`/assistant/interactions/${encodeURIComponent(workspaceId)}`),

  createInteraction: (
    request: {
      mode_id: AssistantModeID
      project_id?: string
      content_id?: string
      resume_interaction_id?: string
      recovery_context?: AssistantInteractionRecoveryContext
    },
    idempotencyKey?: string,
  ) => unwrap<AssistantInteractionSnapshot>(http.post('/assistant/interactions', request, idempotencyKey ? { headers: { 'Idempotency-Key': idempotencyKey } } : undefined)),
  getInteractionSnapshot: (interactionId: string) => unwrap<AssistantInteractionSnapshot>(http.get(`/assistant/interactions/${encodeURIComponent(interactionId)}`)),
  recoverInteraction: (interactionId: string, payload: { generation: number; recovery_context: AssistantInteractionRecoveryContext; continue_conversation?: boolean }) =>
    unwrap<AssistantInteractionSnapshot>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/recover`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  sendInteractionMessage: (interactionId: string, payload: { mode_id: AssistantModeID; generation: number; client_message_id: string; text: string; attachments: Array<{ asset_id: string }> }) => unwrap<{ accepted: boolean }>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/messages`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  answerInteractionQuestion: (
    interactionId: string,
    callId: string,
    payload: {
      mode_id: AssistantModeID
      generation: number
      attempt_id: string
      answers: Array<{ id: string; selected: string[]; custom?: string }>
    },
  ) => unwrap<{ accepted: boolean }>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/answers`, { ...payload, call_id: callId }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  updateInteractionDraft: (interactionId: string, draftId: string, payload: AssistantInteractionDraftUpdateRequest) =>
    unwrap<{
      draft_id: string
      revision: number
      confirmation_required: boolean
      durable: boolean
    }>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/drafts/${encodeURIComponent(draftId)}`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  commitInteraction: (interactionId: string, payload: AssistantInteractionCommitRequest) => unwrap<AssistantInteractionCommitResult>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/commit`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
}

function interactionSnapshotAsWorkspace(snapshot: AssistantInteractionSnapshot): AssistantWorkspace {
  return {
    id: snapshot.interaction_id,
    mode_id: snapshot.mode_id,
    project_id: snapshot.project_id,
    content_id: snapshot.content_id,
    runtime_status: snapshot.runtime_status,
    expires_at: snapshot.expires_at,
    generation: snapshot.generation,
    interaction_snapshot: snapshot,
  }
}
