import { afterEach, describe, expect, it } from 'vitest'
import { http as mockHttp, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { http } from '@/lib/http-client'
import { taskSharesApi } from './task-shares'

const originalBase = http.defaults.baseURL
afterEach(() => { http.defaults.baseURL = originalBase; localStorage.clear() })

describe('taskSharesApi', () => {
  it('reads owner status from a separate API origin without adding unsupported CORS headers', async () => {
    http.defaults.baseURL = 'https://api.anban.test/api/v1'
    localStorage.setItem('anban_creator_token', 'owner-token')
    let requestHeaders: Headers | undefined
    server.use(
      mockHttp.options('https://api.anban.test/api/v1/tasks/task-1/share', () => new HttpResponse(null, { status: 204, headers: {
        'Access-Control-Allow-Origin': window.location.origin,
        'Access-Control-Allow-Methods': 'GET',
        'Access-Control-Allow-Headers': 'Origin,Content-Type,Accept,Authorization,Idempotency-Key,Last-Event-ID',
      } })),
      mockHttp.get('https://api.anban.test/api/v1/tasks/task-1/share', ({ request }) => {
        requestHeaders = request.headers
        return HttpResponse.json({ data: { status: 'none' } }, { headers: { 'Cache-Control': 'no-store', 'Access-Control-Allow-Origin': window.location.origin } })
      }),
    )

    await expect(taskSharesApi.get('task-1')).resolves.toEqual({ status: 'none' })
    expect(requestHeaders?.get('authorization')).toBe('Bearer owner-token')
    expect(requestHeaders?.get('cache-control')).toBeNull()
  })
})
