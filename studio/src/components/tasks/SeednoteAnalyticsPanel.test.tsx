import { fireEvent, screen, waitFor } from '@testing-library/react'
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
  it('shows an empty tracking state when analytics endpoint returns 404', async () => {
    vi.mocked(api.seednoteAnalytics.getByTask).mockRejectedValue({
      response: { status: 404, data: { msg: 'task not found' } },
    })

    render(<SeednoteAnalyticsPanel taskId="task-1" />)

    expect(await screen.findByText('种草笔记数据')).toBeInTheDocument()
    expect(screen.getByText('暂无公开数据，追踪尚未准备')).toBeInTheDocument()
    expect(screen.queryByText('暂时无法加载种草笔记数据')).not.toBeInTheDocument()
  })

  it('binds an unresolved publication with an explicit note URL', async () => {
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({
      tracking: {
        status: 'unresolved',
        run_count: 0,
        last_error: '缺少公开笔记 ID 或链接，尚未建立追踪关联',
      },
      series: [],
    })
    vi.mocked(api.seednoteAnalytics.bind).mockResolvedValue({ tracking: true })

    render(<SeednoteAnalyticsPanel taskId="task-1" />)

    const input = await screen.findByRole('textbox', { name: '公开笔记链接或 ID' })
    expect(screen.getByText('尚未关联公开笔记，请补充笔记链接或 ID')).toBeInTheDocument()
    expect(screen.queryByText(/自动识别|加载中/)).not.toBeInTheDocument()
    fireEvent.change(input, { target: { value: 'https://www.xiaohongshu.com/explore/note-1' } })
    fireEvent.click(screen.getByRole('button', { name: '关联公开笔记' }))

    await waitFor(() => {
      expect(api.seednoteAnalytics.bind).toHaveBeenCalledWith('task-1', {
        note_url: 'https://www.xiaohongshu.com/explore/note-1',
      })
    })
  })
})
