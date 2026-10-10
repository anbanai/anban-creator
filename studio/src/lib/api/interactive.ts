import { http, unwrap } from '@/lib/http-client'

export interface InteractiveAgent {
  id: string
  display_name: string
  description?: string
  capabilities?: string[]
  intents?: string[]
  available?: boolean
}

export interface InteractiveSession {
  id: string
  agent_id: string
  status: string
  expires_at?: string
}

export interface InteractiveOption {
  label: string
  description?: string
}

export interface InteractiveQuestion {
  id: string
  question: string
  header?: string
  detail?: string
  options?: InteractiveOption[]
  multiSelect?: boolean
  multi_select?: boolean
}

export interface InteractiveDraft {
  revision: number
  project?: Record<string, unknown>
  profile?: Record<string, unknown>
  progress?: Record<string, string>
  missing_fields?: string[]
  inferred_fields?: string[]
  ready?: boolean
  name?: string
  fields?: Record<string, unknown>
}

export interface InteractiveCommitResult {
  project?: { id?: string; [key: string]: unknown }
  profile?: Record<string, unknown>
}

export const interactiveApi = {
  agents: () => unwrap<InteractiveAgent[]>(http.get('/interactive/agents')),
  createSession: (agentId: string, intent: string) =>
    unwrap<InteractiveSession>(http.post('/interactive/sessions', { agent_id: agentId, intent })),
  message: (sessionId: string, text: string) =>
    unwrap<unknown>(http.post(`/interactive/sessions/${sessionId}/messages`, { text })),
  answer: (sessionId: string, callId: string, answers: Array<{ id: string; selected: string[]; custom?: string }>) =>
    unwrap<unknown>(http.post(`/interactive/sessions/${sessionId}/questions/${encodeURIComponent(callId)}/answers`, { answers })),
  commit: (sessionId: string, confirmationToken: string, idempotencyKey: string) =>
    unwrap<InteractiveCommitResult>(http.post(`/interactive/sessions/${sessionId}/commit`, {
      confirmation_token: confirmationToken,
      idempotency_key: idempotencyKey,
    })),
  close: (sessionId: string) => http.delete(`/interactive/sessions/${sessionId}`),
}
