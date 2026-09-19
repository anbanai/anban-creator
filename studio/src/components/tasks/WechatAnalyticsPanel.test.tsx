import { screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WechatAnalyticsPanel from './WechatAnalyticsPanel'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: { ...actual.api.tasks, getWechatPublication: vi.fn() },
      wechatAnalytics: { ...actual.api.wechatAnalytics, getByTask: vi.fn() },
    },
  }
})

describe('WechatAnalyticsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'drafted',
    })
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({ series: [] })
  })

  it('keeps draft and publication controls out of the analytics region', async () => {
		render(<WechatAnalyticsPanel taskId="task-1" />)
    await vi.waitFor(() => expect(api.tasks.getWechatPublication).toHaveBeenCalled())
    expect(screen.queryByText('公众号发布')).not.toBeInTheDocument()
    expect(api.wechatAnalytics.getByTask).not.toHaveBeenCalled()
  })

  it('loads the independent analytics region only after publication', async () => {
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'published', article_url: 'https://mp.weixin.qq.com/s/example',
    })
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({
      tracking: {
        source: 'anban_api', status: 'tracking', article_url: 'https://mp.weixin.qq.com/s/example', published_at: '2026-09-17T08:00:00Z',
        expires_at: '2026-10-17T08:00:00Z', run_count: 1,
      },
      series: [],
    })

		render(<WechatAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('公众号数据追踪')).toBeInTheDocument()
    expect(api.wechatAnalytics.getByTask).toHaveBeenCalledWith('task-1')
  })
})
