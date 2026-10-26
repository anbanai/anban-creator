import { parseSSE, type SSEEvent } from '@/lib/sse'
import { streamAssistantInteraction } from '@/lib/api/interactive'

export interface InteractiveStreamEvent extends SSEEvent {
  event: string
}

export interface InteractiveStreamOptions {
  lastEventId?: string
  expectedSessionId?: string
  generation?: number
  onConnected?: () => void
  onEvent?: (event: InteractiveStreamEvent) => void
  endpoint?: string
}

export class InteractiveStreamError extends Error {
  readonly status: number

  constructor(status: number) {
    super(`SSE connection failed: ${status}`)
    this.name = 'InteractiveStreamError'
    this.status = status
  }
}

const TERMINAL_EVENTS = new Set(['session.error', 'session.expired', 'session.closed', 'expired', 'closed'])
const MAX_RECONNECTS = 6
const EVENT_GAP_MESSAGE = '会话历史已失效，请重新开始创建'

function assertEventSequence(lastEventId: string | undefined, eventId: string | undefined) {
  if (!eventId) return
  const current = Number(eventId)
  if (!Number.isSafeInteger(current) || current < 1) return
  if (lastEventId === undefined) return
  const previous = Number(lastEventId)
  if (Number.isSafeInteger(previous) && current > previous + 1) {
    throw new Error(EVENT_GAP_MESSAGE)
  }
}

function waitForRetry(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException('The operation was aborted.', 'AbortError'))
      return
    }
    const timer = window.setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => {
      window.clearTimeout(timer)
      reject(signal.reason ?? new DOMException('The operation was aborted.', 'AbortError'))
    }, { once: true })
  })
}

function retryDelay(reconnects: number): number {
  return Math.min(10_000, 250 * 2 ** Math.min(reconnects, 6))
}

async function retryOrFail(reconnects: number, signal?: AbortSignal) {
  if (reconnects >= MAX_RECONNECTS) {
    throw new Error(`SSE connection failed after ${MAX_RECONNECTS + 1} attempts`)
  }
  await waitForRetry(retryDelay(reconnects), signal)
}

export async function* streamInteractiveSession(sessionId: string, token: string, signal?: AbortSignal, options: InteractiveStreamOptions = {}): AsyncGenerator<InteractiveStreamEvent> {
  let lastEventId: string | undefined = options.lastEventId
  let reconnects = 0
  while (!signal?.aborted) {
    let response: Response
    try {
      response = await fetch(options.endpoint ?? `/api/v1/interactive/sessions/${encodeURIComponent(sessionId)}/events`, {
        headers: {
          Authorization: `Bearer ${token}`,
          Accept: 'text/event-stream',
          ...(lastEventId ? { 'Last-Event-ID': lastEventId } : {}),
          ...(options.expectedSessionId ? { 'X-Assistant-Session-ID': options.expectedSessionId } : {}),
        },
        signal,
      })
    } catch (error) {
      if (signal?.aborted) throw error
      await retryOrFail(reconnects, signal)
      reconnects += 1
      continue
    }
    if (!response.ok) {
      if (response.status === 409) {
        // The server may have compacted the cursor's prefix while the page
        // was backgrounded. Reopen from retained history and reconcile the
        // replay against the persisted client timeline.
        await response.body?.cancel()
        lastEventId = undefined
        reconnects = 0
        continue
      }
      if (response.status === 401 || response.status === 404 || response.status === 410) {
        throw new InteractiveStreamError(response.status)
      }
      await retryOrFail(reconnects, signal)
      reconnects += 1
      continue
    }
    if (!response.body) {
      await retryOrFail(reconnects, signal)
      reconnects += 1
      continue
    }
    options.onConnected?.()

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let terminal = false
    let historyGap = false
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split(/\n\n/)
        buffer = parts.pop() ?? ''
        for (const part of parts) {
          for (const event of parseSSE(`${part}\n\n`)) {
            assertEventSequence(lastEventId, event.id)
            if (event.id) lastEventId = event.id
            reconnects = 0
            terminal ||= TERMINAL_EVENTS.has(event.event)
            const interactiveEvent = event as InteractiveStreamEvent
            options.onEvent?.(interactiveEvent)
            yield interactiveEvent
          }
        }
      }
      if (buffer.trim()) {
        for (const event of parseSSE(`${buffer}\n\n`)) {
          assertEventSequence(lastEventId, event.id)
          if (event.id) lastEventId = event.id
          reconnects = 0
          terminal ||= TERMINAL_EVENTS.has(event.event)
          const interactiveEvent = event as InteractiveStreamEvent
          options.onEvent?.(interactiveEvent)
          yield interactiveEvent
        }
      }
    } catch (error) {
      if (signal?.aborted) throw error
      if (error instanceof Error && error.message === EVENT_GAP_MESSAGE) {
        await reader.cancel(error).catch(() => undefined)
        lastEventId = undefined
        reconnects = 0
        historyGap = true
      }
    } finally {
      reader.releaseLock()
    }
    if (terminal || signal?.aborted) return
    if (historyGap) continue
    await retryOrFail(reconnects, signal)
    reconnects += 1
  }
}

export function streamAssistantWorkspace(workspaceId: string, token: string, signal?: AbortSignal, options: Omit<InteractiveStreamOptions, 'endpoint'> = {}) {
  if (!options.expectedSessionId) {
    return streamAssistantInteraction(workspaceId, token, signal, { generation: options.generation, lastEventId: options.lastEventId, onEvent: options.onEvent }) as AsyncGenerator<InteractiveStreamEvent>
  }
  return streamInteractiveSession(workspaceId, token, signal, {
    ...options,
    endpoint: `/api/v1/assistant/workspaces/${encodeURIComponent(workspaceId)}/events`,
  })
}
