import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { Channel, PaginatedResponse, Plan } from '@/types'

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'test-token' }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      channels: {
        ...actual.api.channels,
        list: vi.fn(),
      },
      plans: {
        ...actual.api.plans,
        list: vi.fn(),
      },
    },
  }
})

const channelWithReferenceImage: Channel = {
  id: 'ch-1',
  user_id: 'user-1',
  platform: 'article',
  name: '有参考图账号',
  avatar_url: '',
  profile_url: '',
  positioning: '测试定位',
  keywords: '测试',
  style: '',
  theme: '',
  author: '作者',
  reference_image_url: 'https://example.com/reference.png',
  image_ratio: '16:9',
  layout: '',
  image_preset: '',
  max_concurrent_tasks: 2,
  config: { wechat_app_id: 'wx123' },
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const planWithReferenceImage: Plan = {
  id: 'plan-1',
  type: 'article',
  title: '测试计划',
  description: '',
  cron_expr: '0 9 * * 1',
  prompt: '测试选题',
  status: 'active',
  next_run_at: '2026-01-05T09:00:00Z',
  channel_id: 'ch-1',
  skip_reference_image: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

describe('PlansPage', () => {
  it('shows the reference image toggle when editing a plan whose channel has a reference image', async () => {
    vi.mocked(api.channels.list).mockResolvedValue([channelWithReferenceImage])
    vi.mocked(api.plans.list).mockResolvedValue({
      items: [planWithReferenceImage],
      total: 1,
    } satisfies PaginatedResponse<Plan>)

    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /使用参考图/ })).toBeInTheDocument()
    })
  })
})
