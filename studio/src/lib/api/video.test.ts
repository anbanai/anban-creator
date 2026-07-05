import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { videoApi } from './video'

describe('videoApi', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    })
  })

  it('posts video config references and unwraps estimate response', async () => {
    let requestBody: unknown
    server.use(
      http.post('/api/v1/video/estimate', async ({ request }) => {
        requestBody = await request.json()
        return HttpResponse.json({
          code: 0,
          msg: 'success',
          data: {
            available_models: [{ key: 'configured-video', display_name: 'Configured Video' }],
            resolved_config: {
              model_key: 'configured-video',
              resolution: '720p',
              ratio: '9:16',
              duration: 5,
              references: [{ type: 'image_url', url: 'https://cdn.example.com/a.png', reference_role: 'product appearance' }],
            },
            estimated_credits: 5000,
            pricing_breakdown: {
              cny: 5,
              credits_per_cny: 1000,
              input_video: false,
              output_seconds: 5,
              resolution: '720p',
              ratio: '9:16',
              model_key: 'configured-video',
            },
            balance: 120000,
            min_balance: 100000,
            meets_min_balance: true,
          },
        })
      }),
    )

    const estimate = await videoApi.estimate({
      project_id: 'project-1',
      prompt: '生成视频',
      video_config: {
        references: [{ type: 'image_url', url: 'https://cdn.example.com/a.png', reference_role: 'product appearance' }],
      },
    })

    expect(requestBody).toEqual({
      project_id: 'project-1',
      prompt: '生成视频',
      video_config: {
        references: [{ type: 'image_url', url: 'https://cdn.example.com/a.png', reference_role: 'product appearance' }],
      },
    })
    expect(estimate.available_models).toHaveLength(1)
    expect(estimate.estimated_credits).toBe(5000)
    expect(estimate.meets_min_balance).toBe(true)
  })

  it('lists configured video models', async () => {
    server.use(
      http.get('/api/v1/video/models', () => HttpResponse.json({
        code: 0,
        msg: 'success',
        data: {
          items: [{ key: 'configured-video', display_name: 'Configured Video' }],
        },
      })),
    )

    const models = await videoApi.models()

    expect(models.items).toEqual([{ key: 'configured-video', display_name: 'Configured Video' }])
  })
})
