export interface SSEEvent {
  event: string
  data: string
  id?: string
}

/**
 * Parse raw SSE text into structured events.
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

  // Handle last event if no trailing newline
  if (dataLines.length > 0) {
    currentEvent.data = dataLines.join('\n')
    events.push(currentEvent as SSEEvent)
  }

  return events
}

/**
 * Stream task lifecycle and log events using fetch + ReadableStream.
 * This approach supports custom Authorization headers, unlike EventSource.
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

      // Process complete SSE events (separated by double newlines)
      const parts = buffer.split('\n\n')
      // Keep the last incomplete part in the buffer
      buffer = parts.pop() ?? ''

      for (const part of parts) {
        const events = parseSSE(part + '\n\n')
        for (const event of events) {
          yield event
        }
      }
    }

    // Process any remaining buffer
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
