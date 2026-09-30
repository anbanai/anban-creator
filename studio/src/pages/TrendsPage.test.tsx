import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import TrendsPage from './TrendsPage'

const { listMock, refreshMock } = vi.hoisted(() => ({
  listMock: vi.fn(),
  refreshMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    projects: { list: vi.fn(async () => []) },
    trends: {
      list: listMock,
      refresh: refreshMock,
    },
    topicPool: { create: vi.fn() },
  },
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

function trendResult() {
  return {
    requested_at: '2026-09-30T08:00:00.000Z',
    ttl_seconds: 300,
    items: [
      {
        platform: 'weibo',
        label: '微博',
        stale: false,
        fetched_at: '2026-09-30T08:00:00.000Z',
        items: [{ title: '热点标题', hot: '1.1985944e+07', url: '', rank: 1 }],
      },
      {
        platform: 'douyin',
        label: '抖音',
        stale: false,
        fetched_at: '2026-09-30T08:00:00.000Z',
        items: [{ title: '抖音热点', hot: '980000', url: '', rank: 1 }],
      },
      {
        platform: 'zhihu',
        label: '知乎',
        stale: false,
        fetched_at: '2026-09-30T08:00:00.000Z',
        items: [{ title: '知乎热点', hot: '12000', url: '', rank: 1 }],
      },
    ],
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  listMock.mockResolvedValue(trendResult())
  refreshMock.mockResolvedValue(trendResult())
})

describe('TrendsPage', () => {
  it('requests the compact core-platform view by default', async () => {
    render(<TrendsPage />)

    await screen.findByText('热点标题')
    expect(listMock).toHaveBeenCalledWith({ platforms: 'weibo,douyin,zhihu', limit: 6 })
    expect(screen.getByRole('button', { name: '微博' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '抖音' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '知乎' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('allows multiple platform tabs to stay active for comparison', async () => {
    render(<TrendsPage />)
    await screen.findByText('热点标题')

    fireEvent.click(screen.getByRole('button', { name: 'B站' }))

    await waitFor(() => expect(listMock).toHaveBeenLastCalledWith({ platforms: 'weibo,douyin,zhihu,bilibili', limit: 6 }))
    expect(screen.getByRole('button', { name: '微博' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'B站' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('formats scientific notation and hides implementation details', async () => {
    render(<TrendsPage />)

    await screen.findByText('热点标题')
    expect(screen.getByText('热度 11,985,944')).toBeInTheDocument()
    expect(screen.queryByText(/TTL|stale|数据有效期内/)).not.toBeInTheDocument()
  })
})
