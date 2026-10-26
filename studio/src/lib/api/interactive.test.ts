import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { streamAssistantInteraction } from './interactive'

vi.mock('@/lib/http-client', () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data,
}))

it('allows interactive commands to wait for long-running runtime responses', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: {} } })

  const { interactiveApi } = await import('./interactive')
  await interactiveApi.message('session-1', '继续整理')
  await interactiveApi.answer('session-1', 'call-1', [{ id: 'choice', selected: ['微信'] }])

  expect(http.post).toHaveBeenNthCalledWith(
    1,
    '/interactive/sessions/session-1/messages',
    { text: '继续整理' },
    { timeout: 15 * 60 * 1000 },
  )
  expect(http.post).toHaveBeenNthCalledWith(
    2,
    '/interactive/sessions/session-1/questions/call-1/answers',
    { answers: [{ id: 'choice', selected: ['微信'] }] },
    { timeout: 15 * 60 * 1000 },
  )
})

function response(body: string) {
  return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } })
}

describe('streamAssistantInteraction', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('ignores events from an older generation while accepting the current generation', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response('event:assistant.message\nid:9\ndata:{"generation":1,"text":"旧事件"}\n\n'))
      .mockResolvedValueOnce(response('event:assistant.message\nid:10\ndata:{"generation":2,"text":"当前事件"}\n\n'))
    const onGenerationChanged = vi.fn()
    const stream = streamAssistantInteraction('interaction-1', 'token', undefined, { generation: 2, onGenerationChanged })

    const next = stream.next()
    await vi.advanceTimersByTimeAsync(250)
    await expect(next).resolves.toMatchObject({ value: { id: '10' }, done: false })
    expect(onGenerationChanged).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
