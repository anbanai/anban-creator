import { http, unwrap } from '@/lib/http-client'

const INTERACTIVE_COMMAND_TIMEOUT_MS = 15 * 60 * 1000

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
  intent?: string
  project_id?: string
  project_name?: string
  status: string
  expires_at?: string
}

export interface InteractiveOption {
  label: string
  description?: string
  recommended?: boolean
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
  payload?: Record<string, unknown>
  project?: Record<string, unknown>
  profile?: Record<string, unknown>
  progress?: Record<string, string>
  missing_fields?: string[]
  inferred_fields?: string[]
  ready?: boolean
  name?: string
  fields?: Record<string, unknown>
  memory_append?: string
}

export interface InteractiveCommitResult {
  project?: { id?: string; [key: string]: unknown }
  profile?: Record<string, unknown>
  task?: { id?: string; project_id?: string; status?: string; [key: string]: unknown }
}

export const interactiveApi = {
  agents: () => unwrap<InteractiveAgent[]>(http.get('/interactive/agents')),
  createSession: (agentId: string, intent: string, projectId?: string, idempotencyKey?: string) =>
    unwrap<InteractiveSession>(http.post('/interactive/sessions', {
      agent_id: agentId,
      intent,
      ...(projectId ? { project_id: projectId } : {}),
    }, idempotencyKey ? { headers: { 'Idempotency-Key': idempotencyKey } } : undefined)),
  message: (sessionId: string, text: string) =>
    unwrap<unknown>(http.post(`/interactive/sessions/${sessionId}/messages`, { text }, { timeout: INTERACTIVE_COMMAND_TIMEOUT_MS })),
  answer: (sessionId: string, callId: string, answers: Array<{ id: string; selected: string[]; custom?: string }>) =>
    unwrap<unknown>(http.post(`/interactive/sessions/${sessionId}/questions/${encodeURIComponent(callId)}/answers`, { answers }, { timeout: INTERACTIVE_COMMAND_TIMEOUT_MS })),
  commit: (sessionId: string, confirmationToken: string, idempotencyKey: string, memoryAppend?: string) =>
    unwrap<InteractiveCommitResult>(http.post(`/interactive/sessions/${sessionId}/commit`, {
      confirmation_token: confirmationToken,
      idempotency_key: idempotencyKey,
      ...(memoryAppend ? { memory_append: memoryAppend } : {}),
    }, { timeout: INTERACTIVE_COMMAND_TIMEOUT_MS })),
  close: (sessionId: string) => http.delete(`/interactive/sessions/${sessionId}`),
}

export interface InteractionEventStreamOptions {
  generation?: number
  lastEventId?: string
  onGenerationChanged?: (generation: number) => void
  onEvent?: (event: { event: string; id?: string; data: string }) => void
}

export interface AssistantInteractionEvent {
  event: string
  id?: string
  data: string
}

export function streamAssistantInteraction(interactionId: string, token: string, signal?: AbortSignal, options: InteractionEventStreamOptions = {}): AsyncGenerator<AssistantInteractionEvent> {
  return (async function* () {
    let cursor = options.lastEventId
    let generation = options.generation ?? 1
    while (!signal?.aborted) {
      const response = await fetch(`/api/v1/assistant/interactions/${encodeURIComponent(interactionId)}/events`, {
        headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream', ...(cursor ? { 'Last-Event-ID': cursor } : {}) },
        signal,
      })
      if (!response.ok) throw new Error(`interaction stream failed: ${response.status}`)
      if (!response.body) return
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      try {
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const chunks = buffer.split('\n\n')
          buffer = chunks.pop() ?? ''
          for (const chunk of chunks) {
            const event = parseInteractionSSE(chunk)
            if (!event) continue
            if (event.id) cursor = event.id
            const data = parseInteractionData(event.data)
            const nextGeneration = typeof data.generation === 'number' ? data.generation : generation
            if (nextGeneration !== generation) {
              generation = nextGeneration
              cursor = undefined
              options.onGenerationChanged?.(generation)
            }
            options.onEvent?.(event)
            yield event
          }
        }
      } finally { reader.releaseLock() }
      if (signal?.aborted) return
      cursor = undefined
    }
  })()
}

function parseInteractionSSE(chunk: string): AssistantInteractionEvent | undefined {
  const lines = chunk.split(/\r?\n/)
  const event = lines.find((line) => line.startsWith('event:'))?.slice(6).trim()
  if (!event) return undefined
  return { event, id: lines.find((line) => line.startsWith('id:'))?.slice(3).trim(), data: lines.filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trim()).join('\n') }
}

function parseInteractionData(data: string | undefined): Record<string, unknown> {
  if (!data) return {}
  try { const value: unknown = JSON.parse(data); return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {} } catch { return {} }
}
