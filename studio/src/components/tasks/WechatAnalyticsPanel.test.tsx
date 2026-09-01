import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import WechatAnalyticsPanel from './WechatAnalyticsPanel'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        getWechatPublication: vi.fn(),
        reconcileWechat: vi.fn(),
        publishWechat: vi.fn(),
        selectWechatArticle: vi.fn(),
      },
      wechatAnalytics: {
        ...actual.api.wechatAnalytics,
        getByTask: vi.fn(),
      },
    },
  }
})

describe('WechatAnalyticsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.getWechatPublication).mockReset().mockResolvedValue({
      id: 'publication-1',
      task_id: 'task-1',
      project_id: 'project-1',
      source: 'wechat_console',
      status: 'drafted',
      draft_media_id: 'draft-media-1',
      draft_title: '每日内容复盘',
    })
    vi.mocked(api.tasks.reconcileWechat).mockResolvedValue({ reconciled: true })
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({ series: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not query lifecycle state when WeChat publishing is disabled', () => {
    render(<WechatAnalyticsPanel taskId="task-1" projectConfig={{ wechat_publish_mode: 'disabled' }} />)

    expect(api.tasks.getWechatPublication).not.toHaveBeenCalled()
    expect(screen.queryByText('公众号发布状态加载中')).not.toBeInTheDocument()
  })

  it('treats a lifecycle 404 as an expected empty state', async () => {
    vi.mocked(api.tasks.getWechatPublication).mockRejectedValue({ response: { status: 404 } })

    render(<WechatAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('该任务尚未创建公众号草稿。')).toBeInTheDocument()
    expect(screen.queryByText('公众号发布状态暂时无法加载，请稍后重试。')).not.toBeInTheDocument()
  })

  it('polls nonterminal lifecycle state until it becomes terminal', async () => {
    vi.useFakeTimers()
    vi.mocked(api.tasks.getWechatPublication)
      .mockResolvedValueOnce({
        id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'publishing',
      })
      .mockResolvedValueOnce({
        id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'published',
      })

    render(<WechatAnalyticsPanel taskId="task-1" />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('微信发布处理中')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '立即检测' })).not.toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(api.tasks.getWechatPublication).toHaveBeenCalledTimes(2)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(api.tasks.getWechatPublication).toHaveBeenCalledTimes(2)
  })

  it('shows lifecycle mutation failures', async () => {
    vi.mocked(api.tasks.reconcileWechat).mockRejectedValue(new Error('provider unavailable'))
    render(<WechatAnalyticsPanel taskId="task-1" />)

    fireEvent.click(await screen.findByRole('button', { name: '立即检测' }))
    expect(await screen.findByText('公众号发布操作失败，请稍后重试。')).toBeInTheDocument()
  })

  it('shows the draft lifecycle controls without any URL binding input', async () => {
    render(<WechatAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('已进入草稿箱')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '打开公众号后台' })).toHaveAttribute('href', 'https://mp.weixin.qq.com/')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '立即检测' }))
    await waitFor(() => expect(api.tasks.reconcileWechat).toHaveBeenCalledWith('task-1'))
  })

  it('shows official metrics automatically after the article is identified', async () => {
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1',
      task_id: 'task-1',
      project_id: 'project-1',
      source: 'wechat_console',
      status: 'published',
      article_url: 'https://mp.weixin.qq.com/s/article-1',
    })
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({
      tracking: {
        status: 'tracking',
        source: 'wechat_console',
        article_url: 'https://mp.weixin.qq.com/s/article-1',
        published_at: '2026-08-13T08:00:00Z',
        expires_at: '2026-09-12T08:00:00Z',
        run_count: 2,
      },
      metrics: {
        read_users: 320,
        share_users: 22,
        collection_users: 16,
        like_users: 10,
        zaikan_users: 8,
        comment_count: 6,
        read_finish_rate: 72,
        average_read_active_time: 86,
        read_to_subscribe_users: 4,
        target_user: 0,
        int_page_read_user: 0,
        int_page_read_count: 0,
        ori_page_read_user: 0,
        ori_page_read_count: 0,
        share_user: 0,
        share_count: 0,
        add_to_fav_user: 0,
        add_to_fav_count: 0,
        stat_date: '2026-08-14',
        captured_at: '2026-08-15T01:00:00Z',
      },
      trend: [],
    })

    render(<WechatAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('正在每日采集官方数据')).toBeInTheDocument()
    expect(screen.getByText('320')).toBeInTheDocument()
    expect(screen.getByText('22')).toBeInTheDocument()
    expect(screen.getByText('16')).toBeInTheDocument()
    expect(screen.getByText('72%')).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })
})
