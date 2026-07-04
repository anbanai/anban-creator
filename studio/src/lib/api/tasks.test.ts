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

  it('posts resume prompt files and labels as form data', async () => {
    let contentType = ''
    let requestBody = ''
    server.use(
      http.post('/api/v1/tasks/task-1/resume', async ({ request }) => {
        contentType = request.headers.get('content-type') ?? ''
        requestBody = await request.text()
        return HttpResponse.json({ data: { id: 'task-1', status: 'pending' } })
      }),
    )
    const file = new File(['notes'], 'notes.md', { type: 'text/markdown' })
    await tasksApi.resume('task-1', {
      prompt: '继续写',
      files: [file],
      fileLabels: ['修改意见'],
    })

    expect(contentType).toContain('multipart/form-data')
    expect(requestBody).toContain('name="prompt"')
    expect(requestBody).toContain('继续写')
    expect(requestBody).toContain('name="file_labels"')
    expect(requestBody).toContain(JSON.stringify(['修改意见']))
    expect(requestBody).toContain('name="files"')
  })
})
