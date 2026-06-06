import { http, unwrap } from '@/lib/http-client'
import { parseSSE } from '@/lib/sse'
import type { SSEEvent } from '@/lib/sse'
import type { DesignerProvider, GenerateRequest, GenerateResult, HistoryResponse, ImageGeneration } from '@/types/designer'

const TOKEN_KEY = 'anbanwriter_token'

export const designerApi = {
  getProviders: () =>
    unwrap<DesignerProvider[]>(http.get('/designer/providers')),

  generate: (req: GenerateRequest) =>
    unwrap<GenerateResult>(http.post('/designer/generate', req, { timeout: 300000 })),

  uploadReference: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return unwrap<{ file_id: string; filename: string; size: number }>(
      http.post('/designer/upload-reference', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      }),
    )
  },

  getHistory: (params: { channel_id?: string; page?: number; page_size?: number } = {}) =>
    unwrap<HistoryResponse>(http.get('/designer/history', { params })),

  getGeneration: (id: string) =>
    unwrap<ImageGeneration>(http.get(`/designer/generations/${id}`)),
}

/**
 * Stream designer image generation via SSE.
 * Yields events: partial (progress), completed (final partial), result (final data), error.
 */
export async function* generateStream(
  req: GenerateRequest,
  signal?: AbortSignal,
): AsyncGenerator<SSEEvent> {
  const token = localStorage.getItem(TOKEN_KEY)
  const baseUrl = import.meta.env.VITE_API_BASE_URL || '/api/v1'

  const response = await fetch(`${baseUrl}/designer/generate`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({ ...req, stream: true }),
    signal,
  })

  if (!response.ok) {
    const body = await response.text().catch(() => '')
    let detail = body
    try {
      const parsed = JSON.parse(body)
      detail = parsed.error || parsed.msg || parsed.message || body
    } catch {}
    throw new Error(`${response.status}${detail ? ` — ${detail}` : ''}`)
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

      const parts = buffer.split('\n\n')
      buffer = parts.pop() ?? ''

      for (const part of parts) {
        const events = parseSSE(part + '\n\n')
        for (const event of events) {
          yield event
        }
      }
    }

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
