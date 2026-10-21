import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AssistantHome from './AssistantHome'
import { createTestQueryClient } from '@/test/test-utils'

const assistantMocks = vi.hoisted(() => ({ bootstrap: vi.fn(), works: vi.fn(), latestCheckpoint: vi.fn() }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, assistant: assistantMocks } }
})

function renderPage() {
  const queryClient = createTestQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <AssistantHome />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AssistantHome', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    assistantMocks.bootstrap.mockResolvedValue({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: true },
      ],
      projects: [
        { id: 'ready-project', name: '已确认画像', platform: 'wechat', profileReady: true, profileRevision: 1, works: [] },
        { id: 'draft-project', name: '未确认画像', platform: 'wechat', profileReady: false, profileRevision: 0, works: [] },
      ],
    })
    assistantMocks.works.mockResolvedValue({ items: [{ id: 'work-1', title: '已完成作品', type: 'article', createdAt: '' }], has_more: false })
    assistantMocks.latestCheckpoint.mockResolvedValue(null)
  })

  it('only offers confirmed profiles in creation and review modes', async () => {
    renderPage()
    const creationSelect = await screen.findByRole('combobox', { name: '选择画像' })
    await waitFor(() => expect(within(creationSelect).getByRole('option', { name: '已确认画像' })).toBeInTheDocument())
    expect(within(creationSelect).queryByRole('option', { name: '未确认画像' })).not.toBeInTheDocument()
    expect(screen.getByText('创作和复盘模式只显示已确认的人设；未显示的人设请先在画像模式完成确认。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /复盘模式/ }))
    const reviewSelect = screen.getByRole('combobox', { name: '选择画像' })
    expect(within(reviewSelect).getByRole('option', { name: '已确认画像' })).toBeInTheDocument()
    expect(within(reviewSelect).queryByRole('option', { name: '未确认画像' })).not.toBeInTheDocument()
    await waitFor(() => expect(assistantMocks.works).toHaveBeenCalledWith('ready-project', undefined, 25))
  })

  it('uses the new persona wording in every mode heading', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { name: '为「已确认画像人设」制定新的创作计划' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /画像模式/ }))
    expect(screen.getByRole('heading', { name: '开始为「新人设」建立画像' })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: '选择画像' }), { target: { value: 'ready-project' } })
    expect(screen.getByRole('heading', { name: '开始为「已确认画像人设」建立画像' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /创作模式/ }))
    expect(screen.getByRole('heading', { name: '为「已确认画像人设」制定新的创作计划' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /复盘模式/ }))
    expect(screen.getByRole('heading', { name: '复盘「已确认画像人设」的作品' })).toBeInTheDocument()
  })

  it('loads review works in pages and exposes more results on demand', async () => {
    assistantMocks.works
      .mockResolvedValueOnce({ items: Array.from({ length: 25 }, (_, index) => ({ id: `work-${index}`, title: `作品 ${index}`, type: 'article', createdAt: '' })), has_more: true, next_cursor: { created_at: '2026-01-01T00:00:24Z', id: 'work-24' } })
      .mockResolvedValueOnce({ items: [{ id: 'work-25', title: '较早作品', type: 'article', createdAt: '' }], has_more: false })

    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /复盘模式/ }))
    await waitFor(() => expect(assistantMocks.works).toHaveBeenCalledWith('ready-project', undefined, 25))
    fireEvent.click(await screen.findByRole('button', { name: '加载更多作品' }))
    await waitFor(() => expect(assistantMocks.works).toHaveBeenCalledWith('ready-project', { created_at: '2026-01-01T00:00:24Z', id: 'work-24' }, 25))
    expect(await screen.findByRole('option', { name: '较早作品' })).toBeInTheDocument()
  })

  it('requests checkpoints only for the selected project and work scope', async () => {
    assistantMocks.latestCheckpoint.mockResolvedValue({ id: 'review-checkpoint', summary: '作品复盘草稿', status: 'draft', updatedAt: '' })
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /复盘模式/ }))
    fireEvent.change(screen.getByRole('combobox', { name: '选择作品' }), { target: { value: 'work-1' } })
    await waitFor(() => expect(assistantMocks.latestCheckpoint).toHaveBeenCalledWith({ mode_id: 'review', project_id: 'ready-project', content_id: 'work-1' }))
    expect(await screen.findByRole('button', { name: '继续草稿' })).toBeInTheDocument()
  })
})
