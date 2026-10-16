import { expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { interactiveApi } from './interactive'

vi.mock('@/lib/http-client', () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data,
}))

it('allows interactive commands to wait for long-running runtime responses', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: {} } })

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
