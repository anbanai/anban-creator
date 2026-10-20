import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import ContentAutopilotSettingsPage from './ContentAutopilotSettingsPage'

const api = vi.hoisted(() => ({ getPolicy: vi.fn(), updatePolicy: vi.fn(), projectsList: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { contentAssets: api, projects: { list: api.projectsList } } }))

describe('ContentAutopilotSettingsPage', () => {
  it('round trips multiple publish windows and per-platform thresholds', async () => {
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
    api.projectsList.mockResolvedValue([{ id: 'p1', name: '公众号', platform: 'wechat', status: 'active' }])
    api.updatePolicy.mockResolvedValue({})
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MemoryRouter><ContentAutopilotSettingsPage /></MemoryRouter></QueryClientProvider>)

    expect(await screen.findByLabelText('发布时间窗口 2')).toHaveValue('18:00')
    expect(screen.getByLabelText('小红书最低推荐分')).toHaveValue(58)
    fireEvent.click(screen.getByLabelText('自动发布'))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(api.updatePolicy).toHaveBeenCalledWith(expect.objectContaining({
      publish_enabled: false,
      publish_windows: [
        { start: '09:00', end: '12:00', timezone: 'Asia/Shanghai' },
        { start: '18:00', end: '21:00', timezone: 'Asia/Shanghai' },
      ],
      platform_min_recommendation_scores: { wechat: 72, seednote: 58 },
      compliance_version: 'v2',
    })))
  })
})
