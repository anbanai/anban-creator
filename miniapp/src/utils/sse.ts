// H5-only SSE streaming over fetch + ReadableStream.
//
// Why not EventSource: the browser EventSource API cannot set custom headers,
// but our /tasks/:id/stream endpoint requires `Authorization: Bearer <jwt>`.
// fetch + ReadableStream lets us stream the SSE body with the auth header, the
// same approach studio uses (studio/src/lib/sse.ts).
//
// On mp-weixin there is no fetch streaming, so real-time falls back to polling
// (see composables/useTaskStream.ts). This module is compiled out of mp-weixin
// via conditional compilation.

// #ifdef H5

export interface SSEEvent {
  event: string
  data: string
  id?: string
}

/**
 * Parse a raw SSE chunk (one or more events separated by blank lines) into
 * structured events. Mirrors studio's parseSSE.
 */
export function parseSSE(text: string): SSEEvent[] {
  const events: SSEEvent[] = []
  const lines = text.split('\n')
  let currentEvent: Partial<SSEEvent> = {}
  let dataLines: string[] = []

  for (const line of lines) {
    if (line.startsWith('event:')) {
      currentEvent.event = line.slice(6).trim()
    } else if (line.startsWith('data:')) {
      dataLines.push(line.slice(5).trim())
    } else if (line.startsWith('id:')) {
      currentEvent.id = line.slice(3).trim()
    } else if (line === '' && dataLines.length > 0) {
      currentEvent.data = dataLines.join('\n')
      events.push(currentEvent as SSEEvent)
      currentEvent = {}
      dataLines = []
    }
  }

  if (dataLines.length > 0) {
    currentEvent.data = dataLines.join('\n')
    events.push(currentEvent as SSEEvent)
  }

  return events
}

/**
 * Stream task progress events from GET /api/v1/tasks/:id/stream as an async
 * generator. Supports the Authorization header. Abort via the signal.
 */
export async function* streamTaskProgress(
  taskId: string,
  token: string,
  signal?: AbortSignal,
): AsyncGenerator<SSEEvent> {
  const response = await fetch(`/api/v1/tasks/${taskId}/stream`, {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  })

  if (!response.ok) {
    throw new Error(`SSE connection failed: ${response.status}`)
  }
  if (!response.body) {
    throw new Error('Response body is null')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })

      // Complete SSE events are separated by a blank line.
      const parts = buffer.split('\n\n')
      buffer = parts.pop() ?? ''

      for (const part of parts) {
        const events = parseSSE(part + '\n\n')
        for (const event of events) {
          yield event
        }
      }
    }

    // Flush any trailing event.
    if (buffer.trim()) {
      const events = parseSSE(buffer + '\n\n')
      for (const event of events) {
        yield event
      }
    }
  } finally {
    reader.releaseLock()
  }
}

// #endif
