import { parseSSE, type SSEEvent } from '@/lib/sse'

export interface InteractiveStreamEvent extends SSEEvent {
  event: string
}

export interface InteractiveStreamOptions {
  lastEventId?: string
  onEvent?: (event: InteractiveStreamEvent) => void
}

const TERMINAL_EVENTS = new Set(['session.error', 'session.expired', 'session.closed', 'expired', 'closed'])

function assertEventSequence(lastEventId: string | undefined, eventId: string | undefined) {
  if (!eventId) return
  const current = Number(eventId)
  if (!Number.isSafeInteger(current) || current < 1) return
  if (lastEventId === undefined) return
  const previous = Number(lastEventId)
  if (Number.isSafeInteger(previous) && current > previous + 1) {
    throw new Error('会话历史已失效，请重新开始创建')
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

export async function* streamInteractiveSession(sessionId: string, token: string, signal?: AbortSignal, options: InteractiveStreamOptions = {}): AsyncGenerator<InteractiveStreamEvent> {
  let lastEventId: string | undefined = options.lastEventId
  let reconnects = 0
  while (!signal?.aborted) {
    let response: Response
    try {
      response = await fetch(`/api/v1/interactive/sessions/${encodeURIComponent(sessionId)}/events`, {
        headers: {
          Authorization: `Bearer ${token}`,
          Accept: 'text/event-stream',
          ...(lastEventId ? { 'Last-Event-ID': lastEventId } : {}),
        },
        signal,
      })
    } catch (error) {
      if (signal?.aborted) throw error
      if (reconnects >= 3) throw new Error('与创作会话的连接已断开')
      await waitForRetry(250 * 2 ** reconnects, signal)
      reconnects += 1
      continue
    }
    if (!response.ok) {
      if (response.status === 409) {
        throw new Error('会话历史已失效，请重新开始创建')
      }
      if (response.status === 401 || response.status === 404 || response.status === 410 || reconnects >= 3) {
        throw new Error(`SSE connection failed: ${response.status}`)
      }
      await waitForRetry(250 * 2 ** reconnects, signal)
      reconnects += 1
      continue
    }
    if (!response.body) throw new Error('Response body is null')

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let terminal = false
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
          terminal ||= TERMINAL_EVENTS.has(event.event)
          const interactiveEvent = event as InteractiveStreamEvent
          options.onEvent?.(interactiveEvent)
          yield interactiveEvent
        }
      }
    } finally {
      reader.releaseLock()
    }
    if (terminal || signal?.aborted) return
    if (reconnects >= 3) throw new Error('与创作会话的连接已断开')
    await waitForRetry(250 * 2 ** reconnects, signal)
    reconnects += 1
  }
}
