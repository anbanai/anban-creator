import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { videoCreatorApi } from './video'

describe('videoCreatorApi', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    })
  })

  it('posts video creator config references and unwraps estimate response', async () => {
    let requestBody: unknown
    server.use(
      http.post('/api/v1/videocreator/estimate', async ({ request }) => {
        requestBody = await request.json()
        return HttpResponse.json({
          code: 0,
          msg: 'success',
          data: {
            available_models: [{ key: 'configured-video', display_name: 'Configured Video' }],
            resolved_creator_config: {
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
            min_balance: 0,
            meets_min_balance: true,
          },
        })
      }),
    )

    const estimate = await videoCreatorApi.estimate({
      project_id: 'project-1',
      prompt: '生成视频',
      video_creator_config: {
        references: [{ type: 'image_url', url: 'https://cdn.example.com/a.png', reference_role: 'product appearance' }],
      },
    })

    expect(requestBody).toEqual({
      project_id: 'project-1',
      prompt: '生成视频',
      video_creator_config: {
        references: [{ type: 'image_url', url: 'https://cdn.example.com/a.png', reference_role: 'product appearance' }],
      },
    })
    expect(estimate.available_models).toHaveLength(1)
    expect(estimate.estimated_credits).toBe(5000)
    expect(estimate.meets_min_balance).toBe(true)
  })

  it('lists configured video models', async () => {
    server.use(
      http.get('/api/v1/videocreator/models', () => HttpResponse.json({
        code: 0,
        msg: 'success',
        data: {
          items: [{ key: 'configured-video', display_name: 'Configured Video' }],
        },
      })),
    )

    const models = await videoCreatorApi.models()

    expect(models.items).toEqual([{ key: 'configured-video', display_name: 'Configured Video' }])
  })

  it('lists video playbooks for scenario-first creation', async () => {
    server.use(
      http.get('/api/v1/videocreator/playbooks', () => HttpResponse.json({
        code: 0,
        msg: 'success',
        data: {
          items: [{
            key: 'live_selling',
            label: '直播带货',
            creative_type: 'product_demo',
            purpose: 'ecommerce',
            required_reference_roles: ['product appearance', 'action'],
            default_ratio: '9:16',
            prompt_scaffold: '黄金三秒开场',
            qc_focus: ['产品保真', 'CTA'],
            risk_notes: ['不要编造优惠'],
          }],
        },
      })),
    )

    const playbooks = await videoCreatorApi.playbooks()

    expect(playbooks.items[0]).toMatchObject({
      key: 'live_selling',
      creative_type: 'product_demo',
      purpose: 'ecommerce',
      default_ratio: '9:16',
    })
  })
})
