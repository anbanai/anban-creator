import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import WechatAnalyticsPanel from './WechatAnalyticsPanel'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      wechatAnalytics: {
        ...actual.api.wechatAnalytics,
        getByTask: vi.fn(),
        bind: vi.fn(),
      },
    },
  }
})

describe('WechatAnalyticsPanel', () => {
  it('binds a public article URL without relying on task published state', async () => {
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({ series: [] })
    vi.mocked(api.wechatAnalytics.bind).mockResolvedValue({ tracking: true })

    render(<WechatAnalyticsPanel taskId="task-1" />)

    const input = await screen.findByRole('textbox', { name: '公众号文章链接' })
    fireEvent.change(input, { target: { value: 'https://mp.weixin.qq.com/s/article-1' } })
    fireEvent.click(screen.getByRole('button', { name: '关联文章' }))

    await waitFor(() => {
      expect(api.wechatAnalytics.bind).toHaveBeenCalledWith('task-1', 'https://mp.weixin.qq.com/s/article-1')
    })
  })

  it('shows official cumulative metrics and tracking status', async () => {
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({
      tracking: {
        status: 'tracking',
        article_url: 'https://mp.weixin.qq.com/s/article-1',
        article_title: '每日内容复盘',
        published_date: '2026-08-13',
        run_count: 2,
      },
      latest: {
        target_user: 1000,
        int_page_read_user: 320,
        int_page_read_count: 480,
        ori_page_read_user: 12,
        ori_page_read_count: 18,
        share_user: 22,
        share_count: 28,
        add_to_fav_user: 16,
        add_to_fav_count: 19,
        stat_date: '2026-08-14',
      },
      deltas: {
        int_page_read_user: 20,
        int_page_read_count: 30,
        share_count: 3,
        add_to_fav_count: 2,
      },
      series: [],
    })

    render(<WechatAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('正在每日采集官方数据')).toBeInTheDocument()
    expect(screen.getByText('每日内容复盘')).toBeInTheDocument()
    expect(screen.getByText('480')).toBeInTheDocument()
    expect(screen.getByText('320')).toBeInTheDocument()
    expect(screen.getByText('28')).toBeInTheDocument()
    expect(screen.getByText('19')).toBeInTheDocument()
  })
})
