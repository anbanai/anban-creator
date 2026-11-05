import { afterEach, describe, expect, it, vi } from 'vitest'
import { http as mockHttp, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { streamTaskProgress } from './sse'
import { streamInteractiveSession } from './interactive-events'
import { streamAssistantInteraction } from './api/interactive'
import { tasksApi } from './api/tasks'
import { readPublicShare, shareImageURL } from './api/task-shares'

const apiBase = 'https://api-creator.anbanai.com/api/v1'

afterEach(() => {
  vi.unstubAllEnvs()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('independent API origin', () => {
  it('refreshes expired authentication and retries REST requests at the API origin', async () => {
    vi.stubEnv('VITE_API_BASE_URL', `${apiBase}/`)
    vi.resetModules()
    const { http } = await import('./http-client')
    localStorage.setItem('anban_creator_token', 'expired-token')
    localStorage.setItem('anban_creator_refresh_token', 'refresh-token')
    const headers = { 'Access-Control-Allow-Origin': window.location.origin }
    const requests: string[] = []
    server.use(
      mockHttp.options(`${apiBase}/*`, () => new HttpResponse(null, { status: 204, headers: {
        ...headers, 'Access-Control-Allow-Methods': 'GET,POST', 'Access-Control-Allow-Headers': 'Content-Type,Authorization',
      } })),
      mockHttp.get(`${apiBase}/projects`, ({ request }) => {
        requests.push(request.headers.get('authorization') ?? '')
        return request.headers.get('authorization') === 'Bearer renewed-token'
          ? HttpResponse.json({ data: [{ id: 'project-1' }] }, { headers })
          : new HttpResponse(null, { status: 401, headers })
      }),
      mockHttp.post(`${apiBase}/auth/refresh`, async ({ request }) => {
        expect(await request.json()).toEqual({ refresh_token: 'refresh-token' })
        expect(request.headers.get('authorization')).toBeNull()
        return HttpResponse.json({ data: { token: 'renewed-token', refresh_token: 'next-refresh-token', user: { id: 'user-1' } } }, { headers })
      }),
    )
    const response = await http.get('/projects')
    expect(response.data.data).toEqual([{ id: 'project-1' }])
    expect(requests).toEqual(['Bearer expired-token', 'Bearer renewed-token'])
    expect(localStorage.getItem('anban_creator_token')).toBe('renewed-token')
  })

  it.each([
    ['task progress', streamTaskProgress, '/tasks/item-1/stream'],
    ['interactive session', streamInteractiveSession, '/interactive/sessions/item-1/events'],
    ['assistant interaction', streamAssistantInteraction, '/assistant/interactions/item-1/events'],
  ] as const)('streams %s directly from the API with bearer authentication', async (_name, openStream, path) => {
    vi.stubEnv('VITE_API_BASE_URL', `${apiBase}/`)
    const fetchRequest = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('event:message\nid:1\ndata:{}\n\n'))
    const stream = openStream('item-1', 'access-token')
    try {
      await expect(stream.next()).resolves.toMatchObject({ value: { event: 'message', id: '1' } })
      expect(fetchRequest).toHaveBeenCalledWith(`${apiBase}${path}`, expect.objectContaining({
        headers: expect.objectContaining({ Authorization: 'Bearer access-token' }),
      }))
    } finally {
      await stream.return(undefined)
    }
  })

  it('uses the configured API for task stream links and anonymous share reads', async () => {
    vi.stubEnv('VITE_API_BASE_URL', `${apiBase}/`)
    expect(tasksApi.streamUrl('task-1')).toBe(`${apiBase}/tasks/task-1/stream`)
    expect(shareImageURL('/api/v1/public/shares/share-1/images/image-1', 'share-1')).toBe(`${apiBase}/public/shares/share-1/images/image-1`)
    const fetchRequest = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json({ data: { title: 'Shared' } }))
    await expect(readPublicShare('share-1', new AbortController().signal)).resolves.toEqual({ title: 'Shared' })
    expect(fetchRequest).toHaveBeenCalledWith(`${apiBase}/public/shares/share-1`, expect.objectContaining({ credentials: 'omit', referrerPolicy: 'no-referrer' }))
  })
})
