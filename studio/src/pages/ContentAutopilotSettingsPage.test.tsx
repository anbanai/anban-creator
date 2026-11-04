import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import ContentAutopilotSettingsPage from './ContentAutopilotSettingsPage'

const api = vi.hoisted(() => ({ getPolicy: vi.fn(), updatePolicy: vi.fn(), projectsList: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { contentAssets: api, projects: { list: api.projectsList } } }))

describe('ContentAutopilotSettingsPage', () => {
  it('keeps expert controls out of the first view and round trips them when opened', async () => {
    api.getPolicy.mockResolvedValue({
      user_id: 'u1', enabled: true, project_ids: ['p1'], platforms: ['wechat'],
      daily_budget_credits: 8000, max_run_cost_credits: 2000, min_recommendation_score: 60,
      platform_min_recommendation_scores: { wechat: 72, seednote: 58 }, min_video_confidence: 75,
      video_first: false,
      publish_windows: [
        { start: '09:00', end: '12:00', timezone: 'Asia/Shanghai' },
        { start: '18:00', end: '21:00', timezone: 'Asia/Shanghai' },
      ], draft_enabled: true, publish_enabled: true, compliance_version: 'v2', failure_mode: 'pause', revision: 3,
    })
    api.projectsList.mockResolvedValue([
      { id: 'p1', name: '公众号', platform: 'wechat', status: 'active' },
      { id: 'p2', name: '小红书账号', platform: 'seednote', status: 'active' },
    ])
    api.updatePolicy.mockResolvedValue({})
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)

    expect(await screen.findByText('选择要自动服务的人设')).toBeInTheDocument()
    expect(screen.getByText('高级设置').closest('details')).not.toHaveAttribute('open')
    expect(screen.getByLabelText('自动发布').closest('details')).not.toHaveAttribute('open')
    expect(screen.queryByText('想生成什么内容？')).not.toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('选择人设 小红书账号'))
    fireEvent.change(screen.getByLabelText('每天最多花费（积分）'), { target: { value: '9000' } })
    fireEvent.click(screen.getByText('高级设置'))
    expect(screen.getByLabelText('单次任务上限（积分）')).toHaveValue(2000)
    expect(await screen.findByLabelText('发布时间窗口 2')).toHaveValue('18:00')
    expect(screen.getByLabelText('小红书最低推荐分')).toHaveValue(58)
    fireEvent.click(screen.getByLabelText('自动发布'))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(api.updatePolicy).toHaveBeenCalledWith(expect.objectContaining({
      publish_enabled: false,
      daily_budget_credits: 9000,
      max_run_cost_credits: 2000,
      project_ids: ['p1', 'p2'],
      platforms: ['wechat', 'seednote'],
      publish_windows: [
        { start: '09:00', end: '12:00', timezone: 'Asia/Shanghai' },
        { start: '18:00', end: '21:00', timezone: 'Asia/Shanghai' },
      ],
      platform_min_recommendation_scores: { wechat: 72, seednote: 58 },
      compliance_version: 'v2',
    })))
  })

  it('requires a persona and budget before autopilot can be enabled or saved', async () => {
    api.getPolicy.mockResolvedValue({ user_id: 'u1', enabled: false, project_ids: [], platforms: [], daily_budget_credits: 0, max_run_cost_credits: 0, min_recommendation_score: 0, min_video_confidence: 0, draft_enabled: false, publish_enabled: false, failure_mode: 'pause', revision: 1 })
    api.projectsList.mockResolvedValue([])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)

    const enabled = await screen.findByLabelText('启用自动研究与生产')
    expect(enabled).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByText('先选择至少一个人设。')).toBeInTheDocument()
    expect(screen.getByText('填写每天的积分上限后，才能开启自动生产。')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '保存设置' })[0]).toBeEnabled()

    api.getPolicy.mockResolvedValue({ user_id: 'u1', enabled: true, project_ids: [], platforms: [], daily_budget_credits: 0, max_run_cost_credits: 0, min_recommendation_score: 0, min_video_confidence: 0, draft_enabled: false, publish_enabled: false, failure_mode: 'pause', revision: 1 })
    const enabledClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={enabledClient}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)
    await screen.findByText('补齐人设和每天的积分上限，或先关闭自动生产，再保存设置。')
    expect(screen.getAllByRole('button', { name: '保存设置' })[1]).toBeDisabled()
    fireEvent.click(screen.getAllByLabelText('启用自动研究与生产')[1])
    expect(screen.getAllByRole('button', { name: '保存设置' })[1]).toBeEnabled()
    fireEvent.click(screen.getAllByRole('button', { name: '保存设置' })[1])
    await waitFor(() => expect(api.updatePolicy).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false })))
  })

  it('fills the hidden per-run cap from the daily budget when no cap is configured', async () => {
    api.getPolicy.mockResolvedValue({ user_id: 'u1', enabled: false, project_ids: ['p1'], platforms: ['wechat'], daily_budget_credits: 0, max_run_cost_credits: 0, min_recommendation_score: 0, min_video_confidence: 0, draft_enabled: false, publish_enabled: false, failure_mode: 'pause', revision: 1 })
    api.projectsList.mockResolvedValue([{ id: 'p1', name: '公众号人设', platform: 'wechat', status: 'active' }])
    api.updatePolicy.mockResolvedValue({ revision: 2 })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)

    const dailyBudget = await screen.findByLabelText('每天最多花费（积分）')
    fireEvent.change(dailyBudget, { target: { value: '5' } })
    fireEvent.change(dailyBudget, { target: { value: '5000' } })
    expect(screen.getByLabelText('启用自动研究与生产')).not.toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByLabelText('启用自动研究与生产'))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(api.updatePolicy).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      daily_budget_credits: 5000,
      max_run_cost_credits: 5000,
    })))
  })

  it('rejects fractional daily budgets even when autopilot is disabled', async () => {
    api.getPolicy.mockResolvedValue({ user_id: 'u1', enabled: false, project_ids: [], platforms: [], daily_budget_credits: 0, max_run_cost_credits: 0, min_recommendation_score: 0, min_video_confidence: 0, draft_enabled: false, publish_enabled: false, failure_mode: 'pause', revision: 1 })
    api.projectsList.mockResolvedValue([])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)

    fireEvent.change(await screen.findByLabelText('每天最多花费（积分）'), { target: { value: '12.5' } })

    expect(await screen.findByText('每天的积分上限必须是非负整数。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存设置' })).toBeDisabled()
    expect(api.updatePolicy).not.toHaveBeenCalled()
  })
})
