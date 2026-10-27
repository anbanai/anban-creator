import { http, unwrap } from '@/lib/http-client'
import type { InteractiveCommitResult, InteractiveSession } from './interactive'
import type { VideoEditDefaults } from '@/types/video-edit'

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
  options?: Record<string, unknown>
}

export interface AssistantInteractionCommitResult {
  status: 'created' | 'already_created' | 'needs_reconciliation' | string
  task_id?: string
  result?: Record<string, unknown>
}

export interface AssistantCheckpointSummary {
  id: string
  summary: string
  status: 'draft' | 'ready' | 'committed' | 'abandoned' | string
  updatedAt: string
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

export interface CreateAssistantSessionRequest {
  mode_id: AssistantModeID
  project_id?: string
  content_id?: string
  checkpoint_id?: string
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

export interface AssistantCheckpoint extends AssistantCheckpointRequest {
  id: string
  payload_digest: string
  user_id: string
  mode_id: AssistantModeID
  project_id?: string
  content_id?: string
  source_session_id: string
  expires_at: string
  revision?: number
}

export type AssistantCommitRequest = { idempotency_key: string; options?: Record<string, unknown> } & (
  | { confirmation_token: string; video_edit_defaults?: VideoEditDefaults }
  | { checkpoint_id: string; checkpoint_digest: string }
)

export type AssistantSession = InteractiveSession & {
  mode_id?: AssistantModeID
  content_id?: string
}

export const assistantApi = {
  bootstrap: () => unwrap<AssistantBootstrap>(http.get('/assistant/bootstrap')),

  works: (projectId: string, cursor?: { created_at: string; id: string }, limit = 25) => unwrap<AssistantWorkPage>(http.get(
    `/assistant/projects/${encodeURIComponent(projectId)}/works`,
    { params: { ...(cursor ? { before_created_at: cursor.created_at, before_id: cursor.id } : {}), limit } },
  )),

  latestCheckpoint: (scope: { mode_id: AssistantModeID; project_id?: string; content_id?: string }) =>
    unwrap<AssistantCheckpointSummary | null>(http.get('/assistant/checkpoint', {
      params: scope,
    })),

  createSession: (request: CreateAssistantSessionRequest, idempotencyKey?: string) =>
    unwrap<AssistantSession>(http.post('/assistant/sessions', request, idempotencyKey
      ? { headers: { 'Idempotency-Key': idempotencyKey } }
      : undefined)),

  message: (sessionId: string, text: string) =>
    unwrap<unknown>(http.post(`/assistant/sessions/${encodeURIComponent(sessionId)}/messages`, { text }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  answer: (sessionId: string, callId: string, answers: Array<{ id: string; selected: string[]; custom?: string }>) =>
    unwrap<unknown>(http.post(`/assistant/sessions/${encodeURIComponent(sessionId)}/questions/${encodeURIComponent(callId)}/answers`, { answers }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  checkpoint: (sessionId: string, checkpoint: AssistantCheckpointRequest) =>
    unwrap<AssistantCheckpoint>(http.post(`/assistant/sessions/${encodeURIComponent(sessionId)}/checkpoint`, checkpoint, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  commit: (sessionId: string, payload: AssistantCommitRequest) =>
    unwrap<InteractiveCommitResult & Record<string, unknown>>(http.post(`/assistant/sessions/${encodeURIComponent(sessionId)}/commit`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  close: (sessionId: string) => http.delete(`/assistant/sessions/${encodeURIComponent(sessionId)}`),

  createWorkspace: async (request: { mode_id: AssistantModeID; project_id?: string; content_id?: string; checkpoint_id?: string }, idempotencyKey?: string) =>
    interactionSnapshotAsWorkspace(await assistantApi.createInteraction({ mode_id: request.mode_id, project_id: request.project_id, content_id: request.content_id }, idempotencyKey)),

  switchWorkspaceMode: (workspaceId: string, modeId: AssistantModeID, context?: { project_id?: string; content_id?: string; checkpoint_id?: string }) =>
    assistantApi.createInteraction({ mode_id: modeId, project_id: context?.project_id, content_id: context?.content_id, resume_interaction_id: workspaceId }).then(interactionSnapshotAsWorkspace),

  workspaceMessage: async (workspaceId: string, _modeId: AssistantModeID, text: string) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    return assistantApi.sendInteractionMessage(workspaceId, { generation: snapshot.generation, client_message_id: crypto.randomUUID(), text })
  },

  workspaceAnswer: async (workspaceId: string, _modeId: AssistantModeID, callId: string, answers: Array<{ id: string; selected: string[]; custom?: string }>) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    return assistantApi.answerInteractionQuestion(workspaceId, callId, { generation: snapshot.generation, attempt_id: crypto.randomUUID(), answers })
  },

  workspaceCheckpoint: async (workspaceId: string, modeId: AssistantModeID, checkpoint: AssistantCheckpointRequest) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    const draft = [...snapshot.drafts].filter((item) => item.mode_id === modeId).sort((a, b) => b.revision - a.revision)[0]
    if (!draft) throw new Error('当前计划尚未保存，请重试暂存')
    return { id: draft.draft_id, payload_digest: draft.payload_digest, revision: draft.revision, schema_version: checkpoint.schema_version, payload: draft.payload, summary: checkpoint.summary, status: draft.ready ? 'ready' : 'draft', user_id: '', mode_id: modeId, project_id: snapshot.project_id, content_id: snapshot.content_id, source_session_id: workspaceId, expires_at: snapshot.expires_at } satisfies AssistantCheckpoint
  },

  workspaceCommit: async (workspaceId: string, _modeId: AssistantModeID, payload: AssistantCommitRequest) => {
    const snapshot = await assistantApi.getInteractionSnapshot(workspaceId)
    const requestedID = 'checkpoint_id' in payload ? payload.checkpoint_id : undefined
    const draft = snapshot.drafts.find((item) => item.draft_id === requestedID) ?? snapshot.drafts.find((item) => item.ready)
    if (!draft) throw new Error('当前计划尚未保存，请恢复计划')
    const result = await assistantApi.commitInteraction(workspaceId, { draft_id: draft.draft_id, revision: draft.revision, idempotency_key: payload.idempotency_key, options: payload.options })
    if (result.result && typeof result.result === 'object') return { ...result.result as InteractiveCommitResult & Record<string, unknown>, interaction_status: result.status, task_id: result.task_id } as InteractiveCommitResult & Record<string, unknown>
    return { interaction_status: result.status, task_id: result.task_id, task: result.task_id ? { id: result.task_id } : undefined }
  },

  closeWorkspace: (workspaceId: string) => http.delete(`/assistant/interactions/${encodeURIComponent(workspaceId)}`),

  createInteraction: (request: { mode_id: AssistantModeID; project_id?: string; content_id?: string; resume_interaction_id?: string }, idempotencyKey?: string) =>
    unwrap<AssistantInteractionSnapshot>(http.post('/assistant/interactions', request, idempotencyKey ? { headers: { 'Idempotency-Key': idempotencyKey } } : undefined)),
  getInteractionSnapshot: (interactionId: string) =>
    unwrap<AssistantInteractionSnapshot>(http.get(`/assistant/interactions/${encodeURIComponent(interactionId)}`)),
  sendInteractionMessage: (interactionId: string, payload: { generation: number; client_message_id: string; text: string }) =>
    unwrap<{ accepted: boolean }>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/messages`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  answerInteractionQuestion: (interactionId: string, callId: string, payload: { generation: number; attempt_id: string; answers: Array<{ id: string; selected: string[]; custom?: string }> }) =>
    unwrap<{ accepted: boolean }>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/answers`, { ...payload, call_id: callId }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
  commitInteraction: (interactionId: string, payload: AssistantInteractionCommitRequest) =>
    unwrap<AssistantInteractionCommitResult>(http.post(`/assistant/interactions/${encodeURIComponent(interactionId)}/commit`, payload, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),
}

function interactionSnapshotAsWorkspace(snapshot: AssistantInteractionSnapshot): AssistantWorkspace {
  return { id: snapshot.interaction_id, mode_id: snapshot.mode_id, project_id: snapshot.project_id, content_id: snapshot.content_id, runtime_status: snapshot.runtime_status, expires_at: snapshot.expires_at, generation: snapshot.generation, interaction_snapshot: snapshot }
}
