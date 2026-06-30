import { screen } from '@testing-library/react'
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
})
