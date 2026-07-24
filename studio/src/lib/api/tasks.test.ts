import { afterEach, beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { http as clientHttp } from '@/lib/http-client'
import { tasksApi } from './tasks'
import type { CreateTaskRequest } from '@/types'

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

  it('posts resume prompt and stable OSS attachment identities as JSON', async () => {
    const post = vi.spyOn(clientHttp, 'post').mockResolvedValue({ data: { data: { id: 'task-1', status: 'pending' } } } as any)
    await tasksApi.resume('task-1', {
      prompt: '继续写',
      input_attachments: [{
        type: 'text',
        upload_id: 'upload-notes',
        key: 'uploads/pending/user-1/upload-notes/notes.md',
        file_name: 'notes.md',
        content_type: 'text/markdown',
        size: 5,
        instruction: '修改意见',
      }],
    })

    expect(post).toHaveBeenCalledWith('/tasks/task-1/resume', {
      prompt: '继续写',
      input_attachments: [{
        type: 'text',
        upload_id: 'upload-notes',
        key: 'uploads/pending/user-1/upload-notes/notes.md',
        file_name: 'notes.md',
        content_type: 'text/markdown',
        size: 5,
        instruction: '修改意见',
      }],
    })
  })

  it('posts the complete clone request unchanged', async () => {
    const post = vi.spyOn(clientHttp, 'post').mockResolvedValue({ data: { data: { id: 'task-clone', status: 'pending' } } } as any)
    const request: CreateTaskRequest = {
      type: 'montage',
      project_id: 'project-1',
      prompt: '',
      quantity: 1,
      image_ratio: '9:16',
      image_model_key: 'video-model',
      skip_reference_image: false,
      reference_image: { asset_id: 'asset-1' },
      input_attachments: [],
      watermark: false,
      goal: '',
      goal_mode: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      product_photos: ['oss://product.png'],
      selected_modules: { hero: 1 },
      target_platform: 'douyin',
      selling_points: '',
      language: 'zh-CN',
      montage_input: {
        brief: '',
        source_assets: [],
        delivery_targets: [],
        advanced: { render: { fps: 30 } },
      },
      execution_target: 'local',
    }

    await tasksApi.clone('task-1', request)

    expect(post).toHaveBeenCalledWith('/tasks/task-1/clone', request)
  })
})
