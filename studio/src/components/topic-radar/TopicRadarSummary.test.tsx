import { act, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { TopicRadarSummary } from './TopicRadarSummary'

const radar = vi.hoisted(() => ({ subscriptions: vi.fn(), digests: vi.fn(), digest: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { topicRadar: radar } }))

describe('TopicRadarSummary', () => {
  afterEach(() => vi.useRealTimers())

  it('stops polling after the detail reaches terminal status', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    radar.subscriptions.mockResolvedValue({ items: [{ id: 'running-sub' }] })
    radar.digests.mockResolvedValue({ items: [{ id: 'running-digest', status: 'running', created_at: '2026-10-31T09:00:00Z' }] })
    radar.digest.mockResolvedValue({ digest: { id: 'running-digest', status: 'succeeded' }, suggestions: [{ id: 'done', title: '完成后停止刷新' }] })
    render(<TopicRadarSummary projectId="running-project" />)
    expect(await screen.findByText('完成后停止刷新')).toBeInTheDocument()
    const calls = radar.digest.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(radar.digest).toHaveBeenCalledTimes(calls)
  })

  it('shows the newest digest across subscriptions and its scoped source link', async () => {
    radar.subscriptions.mockResolvedValue({ items: [{ id: 'sub-1' }, { id: 'sub-2' }] })
    radar.digests.mockImplementation((id: string) => Promise.resolve({ items: [{ id: id === 'sub-1' ? 'old' : 'new', status: 'succeeded', created_at: id === 'sub-1' ? '2026-10-30T09:00:00Z' : '2026-10-31T09:00:00Z' }] }))
    radar.digest.mockImplementation((id: string) => Promise.resolve({ digest: { id }, suggestions: [{ id: 'suggestion', title: id === 'new' ? '最新自动入库选题' : '旧选题' }], adoptions: [{ status: 'adopted' }] }))
    render(<TopicRadarSummary projectId="project-1" />)
    expect(await screen.findByText('最新自动入库选题')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看本次分析' })).toHaveAttribute('href', '/topics?project_id=project-1&detail=discover&run=new')
    expect(screen.queryByRole('button', { name: /采纳/ })).not.toBeInTheDocument()
  })

  it('offers the selected project entry when no subscription exists', async () => {
    radar.subscriptions.mockResolvedValue({ items: [] })
    render(<TopicRadarSummary projectId="empty-project" />)
    expect(await screen.findByText('为当前人设启用每日选题推荐。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开选题库' })).toHaveAttribute('href', '/topics?project_id=empty-project')
  })
})
