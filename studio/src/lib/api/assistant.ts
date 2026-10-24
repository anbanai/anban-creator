import { http, unwrap } from '@/lib/http-client'
import type { InteractiveCommitResult, InteractiveSession } from './interactive'
import type { VideoEditDefaults } from '@/types/video-edit'

const ASSISTANT_COMMAND_TIMEOUT_MS = 15 * 60 * 1000

export type AssistantModeID = 'profile' | 'creation' | 'review'

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
  session_id: string
  sessions: Partial<Record<AssistantModeID, string>>
  expires_at: string
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
}

export type AssistantCommitRequest = { idempotency_key: string } & (
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

  createWorkspace: (request: { mode_id: AssistantModeID; project_id?: string; content_id?: string; checkpoint_id?: string }, idempotencyKey?: string) =>
    unwrap<AssistantWorkspace>(http.post('/assistant/workspaces', request, idempotencyKey ? { headers: { 'Idempotency-Key': idempotencyKey } } : undefined)),

  switchWorkspaceMode: (workspaceId: string, modeId: AssistantModeID, context?: { project_id?: string; content_id?: string; checkpoint_id?: string }) =>
    unwrap<AssistantWorkspace>(http.post(`/assistant/workspaces/${encodeURIComponent(workspaceId)}/mode`, { mode_id: modeId, ...context })),

  workspaceMessage: (workspaceId: string, modeId: AssistantModeID, text: string) =>
    unwrap<unknown>(http.post(`/assistant/workspaces/${encodeURIComponent(workspaceId)}/messages`, { mode_id: modeId, text }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  workspaceAnswer: (workspaceId: string, modeId: AssistantModeID, callId: string, answers: Array<{ id: string; selected: string[]; custom?: string }>) =>
    unwrap<unknown>(http.post(`/assistant/workspaces/${encodeURIComponent(workspaceId)}/questions/${encodeURIComponent(callId)}/answers`, { mode_id: modeId, answers }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  workspaceCheckpoint: (workspaceId: string, modeId: AssistantModeID, checkpoint: AssistantCheckpointRequest) =>
    unwrap<AssistantCheckpoint>(http.post(`/assistant/workspaces/${encodeURIComponent(workspaceId)}/checkpoint`, { mode_id: modeId, ...checkpoint }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  workspaceCommit: (workspaceId: string, modeId: AssistantModeID, payload: AssistantCommitRequest) =>
    unwrap<InteractiveCommitResult & Record<string, unknown>>(http.post(`/assistant/workspaces/${encodeURIComponent(workspaceId)}/commit`, { mode_id: modeId, ...payload }, { timeout: ASSISTANT_COMMAND_TIMEOUT_MS })),

  closeWorkspace: (workspaceId: string) => http.delete(`/assistant/workspaces/${encodeURIComponent(workspaceId)}`),
}
