import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import ContentAssetsPage from './ContentAssetsPage'

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), createReuseRun: vi.fn(), getRun: vi.fn(), setRunStatus: vi.fn(), getPolicy: vi.fn(), updatePolicy: vi.fn(), projectsList: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { contentAssets: api, projects: { list: api.projectsList } } }))

describe('ContentAssetsPage', () => {
  it('uses user-facing 选题资产 language and can create an asset', async () => {
    api.list.mockResolvedValue({ items: [{ id: 'a1', user_id: 'u1', title: '努力与结果', core_question: '如何找到正确方向', lifecycle: 'evergreen', status: 'candidate', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }], total: 1 })
    api.create.mockResolvedValue({})
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAssetsPage /></MemoryRouter></QueryClientProvider>)
    expect(await screen.findByRole('heading', { name: '选题资产' })).toBeInTheDocument()
    expect(await screen.findByText('努力与结果')).toBeInTheDocument()
    expect(screen.queryByText('母题')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '新增选题资产' }))
    fireEvent.change(screen.getByLabelText('选题标题'), { target: { value: '新的长期选题' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.create).toHaveBeenCalledWith({ title: '新的长期选题' }))
  })

  it('creates a cross-platform reuse run and supports pause and resume', async () => {
    api.list.mockResolvedValue({ items: [{ id: 'a1', user_id: 'u1', title: '努力与结果', lifecycle: 'evergreen', status: 'ready', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }], total: 1 })
    api.projectsList.mockResolvedValue([{ id: 'p1', name: '我的视频号', platform: 'hypit', status: 'active' }])
    api.getPolicy.mockResolvedValue({ enabled: false, publish_enabled: false, daily_budget_credits: 0, revision: 1 })
    api.createReuseRun.mockResolvedValue({ id: 'r1', asset_id: 'a1', target_project_id: 'p1', target_platform: 'hypit', stage: 'research', status: 'queued' })
    api.setRunStatus.mockResolvedValue({ id: 'r1', asset_id: 'a1', target_platform: 'hypit', stage: 'research', status: 'paused' })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAssetsPage /></MemoryRouter></QueryClientProvider>)

    fireEvent.click(await screen.findByRole('button', { name: '跨平台复用' }))
    await screen.findByRole('option', { name: /我的视频号/ })
    fireEvent.change(screen.getByLabelText('目标项目'), { target: { value: 'p1' } })
    fireEvent.click(screen.getByRole('button', { name: '开始复用' }))
    await waitFor(() => expect(api.createReuseRun).toHaveBeenCalledWith('a1', expect.objectContaining({ target_project_id: 'p1', target_platform: 'hypit' })))
    expect(await screen.findByText(/等待自动生产/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    await waitFor(() => expect(api.setRunStatus).toHaveBeenCalledWith('r1', 'paused'))
    expect(await screen.findByText(/已暂停/)).toBeInTheDocument()
  })

  it('refreshes the reuse run and shows server-side failure details', async () => {
    api.list.mockResolvedValue({ items: [{ id: 'a1', user_id: 'u1', title: '努力与结果', lifecycle: 'evergreen', status: 'ready', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }], total: 1 })
    api.projectsList.mockResolvedValue([{ id: 'p1', name: '公众号', platform: 'wechat', status: 'active' }])
    api.getPolicy.mockResolvedValue({ enabled: false, publish_enabled: false, daily_budget_credits: 0, revision: 1 })
    api.createReuseRun.mockResolvedValue({ id: 'r2', asset_id: 'a1', target_project_id: 'p1', target_platform: 'wechat', stage: 'research', status: 'queued' })
    api.getRun.mockResolvedValue({ id: 'r2', asset_id: 'a1', target_platform: 'wechat', stage: 'produce', status: 'failed', error_code: 'task_creation_failed', error_message: '任务创建失败' })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAssetsPage /></MemoryRouter></QueryClientProvider>)

    fireEvent.click(await screen.findByRole('button', { name: '跨平台复用' }))
    fireEvent.change(await screen.findByLabelText('目标项目'), { target: { value: 'p1' } })
    fireEvent.click(screen.getByRole('button', { name: '开始复用' }))

    expect(await screen.findByText(/生产失败 · 内容生成/)).toBeInTheDocument()
    expect(await screen.findByText('任务创建失败')).toBeInTheDocument()
    expect(api.getRun).toHaveBeenCalledWith('r2')
  })
})
