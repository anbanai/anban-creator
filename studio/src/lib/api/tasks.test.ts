import { afterEach, beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { http as clientHttp } from '@/lib/http-client'
import { tasksApi } from './tasks'

describe('tasksApi', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
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

  it('preserves the serialized execution result returned by the task API', async () => {
    const serializedResult = JSON.stringify({ success: true, log_text: 'done' })
    server.use(
      http.get('/api/v1/tasks/task-contract', () => HttpResponse.json({
        code: 0,
        msg: 'ok',
        data: {
          id: 'task-contract',
          type: 'article',
          prompt: 'contract fixture',
          status: 'completed',
          project_id: 'project-1',
          result: serializedResult,
          published: false,
          published_at: null,
          created_at: '2026-07-15T00:00:00Z',
          started_at: '2026-07-15T00:00:01Z',
          completed_at: '2026-07-15T00:01:00Z',
        },
      })),
    )

    const task = await tasksApi.get('task-contract')

    expect(task.result).toBe(serializedResult)
    expect(typeof task.result).toBe('string')
    expectTypeOf(task.result).toEqualTypeOf<string | null | undefined>()
  })

  it('posts resume prompt files and labels as form data', async () => {
    const post = vi.spyOn(clientHttp, 'post').mockResolvedValue({ data: { data: { id: 'task-1', status: 'pending' } } } as any)
    const file = new File(['notes'], 'notes.md', { type: 'text/markdown' })
    await tasksApi.resume('task-1', {
      prompt: '继续写',
      files: [file],
      fileLabels: ['修改意见'],
    })

    expect(post).toHaveBeenCalledWith('/tasks/task-1/resume', expect.any(FormData), {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    const form = post.mock.calls[0][1] as FormData
    expect(form.get('prompt')).toBe('继续写')
    expect(form.get('file_labels')).toBe(JSON.stringify(['修改意见']))
    const submittedFile = form.get('files')
    expect(submittedFile).toBeInstanceOf(File)
    expect((submittedFile as File).name).toBe('notes.md')
  })

  it('always posts the complete clone input snapshot including empty fields', async () => {
    const post = vi.spyOn(clientHttp, 'post').mockResolvedValue({ data: { data: { id: 'task-clone', status: 'pending' } } } as any)

    await tasksApi.clone('task-1', {
      prompt: '',
      input_attachments: [],
    })

    expect(post).toHaveBeenCalledWith('/tasks/task-1/clone', {
      prompt: '',
      input_attachments: [],
    })
  })
})
