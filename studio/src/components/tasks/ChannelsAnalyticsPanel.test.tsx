import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import ChannelsAnalyticsPanel from './ChannelsAnalyticsPanel'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      channelsAnalytics: {
        ...actual.api.channelsAnalytics,
        getByTask: vi.fn(),
        bind: vi.fn(),
      },
    },
  }
})

describe('ChannelsAnalyticsPanel', () => {
  it('binds a public video URL and explains immediate collection', async () => {
    vi.mocked(api.channelsAnalytics.getByTask).mockResolvedValue({ series: [] })
    vi.mocked(api.channelsAnalytics.bind).mockResolvedValue({ tracking: true })

    render(<ChannelsAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText(/立即获取首条数据/)).toBeInTheDocument()
    const input = screen.getByRole('textbox', { name: '视频号视频链接' })
    fireEvent.change(input, { target: { value: 'https://weixin.qq.com/sph/video-1' } })
    fireEvent.click(screen.getByRole('button', { name: '关联并获取' }))

    await waitFor(() => {
      expect(api.channelsAnalytics.bind).toHaveBeenCalledWith('task-1', 'https://weixin.qq.com/sph/video-1')
    })
    expect(screen.getByText(/不包含播放量/)).toBeInTheDocument()
  })

  it('shows third-party cumulative metrics and tracking status', async () => {
    vi.mocked(api.channelsAnalytics.getByTask).mockResolvedValue({
      tracking: {
        status: 'tracking',
        video_url: 'https://weixin.qq.com/sph/video-1',
        video_title: '视频号内容复盘',
        author_name: '安伴视频号',
        run_count: 2,
        provider_name: '世界树科技',
      },
      latest: {
        like_count: 120,
        favorite_count: 18,
        comment_count: 9,
        forward_count: 25,
      },
      deltas: {
        like_count: 10,
        favorite_count: 2,
        comment_count: 1,
        forward_count: 3,
      },
      series: [],
    })

    render(<ChannelsAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('正在每日采集')).toBeInTheDocument()
    expect(screen.getByText('视频号内容复盘')).toBeInTheDocument()
    expect(screen.getByText('第三方来源：世界树科技')).toBeInTheDocument()
    expect(screen.getByText('120')).toBeInTheDocument()
    expect(screen.getByText('18')).toBeInTheDocument()
    expect(screen.getByText('9')).toBeInTheDocument()
    expect(screen.getByText('25')).toBeInTheDocument()
  })
})
