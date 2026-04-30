import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { tasksApi } from './tasks'

describe('tasksApi', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    })
  })

  it('posts selected task ids when downloading a bulk zip', async () => {
    let requestBody: unknown
    server.use(
      http.post('/api/v1/tasks/files/zip', async ({ request }) => {
        requestBody = await request.json()
        return HttpResponse.arrayBuffer(new TextEncoder().encode('zip').buffer)
      }),
    )

    const blob = await tasksApi.downloadBulkZipBlob(['task-1', 'task-2'])

    expect(requestBody).toEqual({ task_ids: ['task-1', 'task-2'] })
    expect(await blob.text()).toBe('zip')
  })
})
