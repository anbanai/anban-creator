import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import SeednoteAnalyticsPanel from './SeednoteAnalyticsPanel'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      seednoteAnalytics: {
        ...actual.api.seednoteAnalytics,
        getByTask: vi.fn(),
        bind: vi.fn(),
      },
    },
  }
})

describe('SeednoteAnalyticsPanel', () => {
  it('does not show a public note binding form when tracking is empty', async () => {
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({
      series: [],
    })

    render(<SeednoteAnalyticsPanel taskId="task-1" />)

    await waitFor(() => {
      expect(screen.queryByText('种草笔记公开数据加载中')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('种草笔记数据')).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '公开笔记链接或 ID' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关联公开笔记' })).not.toBeInTheDocument()
  })

  it('shows an empty tracking state when analytics endpoint returns 404', async () => {
    vi.mocked(api.seednoteAnalytics.getByTask).mockRejectedValue({
      response: { status: 404, data: { msg: 'task not found' } },
    })

    render(<SeednoteAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('种草笔记数据')).toBeInTheDocument()
    expect(screen.getByText('暂无公开数据，追踪尚未准备')).toBeInTheDocument()
    expect(screen.queryByText('暂时无法加载种草笔记数据')).not.toBeInTheDocument()
  })

  it('does not show a public note binding form for unresolved tracking', async () => {
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({
      tracking: {
        status: 'unresolved',
        run_count: 0,
        last_error: '缺少公开笔记 ID 或链接，尚未建立追踪关联',
      },
      series: [],
    })
    render(<SeednoteAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('种草笔记数据')).toBeInTheDocument()
    expect(screen.getByText('尚未关联公开笔记')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '公开笔记链接或 ID' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关联公开笔记' })).not.toBeInTheDocument()
  })
})
