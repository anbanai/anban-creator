import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { mockProjects, mockProjectDetail } from '@/test/mocks/handlers'
import { normalizeProjectProfile } from '@/lib/project-profile'
import PortraitDetailPage from './PortraitDetailPage'

vi.mock('@/lib/api', () => ({ api: { projects: { get: vi.fn(), getAccountProfile: vi.fn(), updateProfileDimension: vi.fn(), update: vi.fn() } } }))
const profile = normalizeProjectProfile({ status: 'confirmed', version: 3, dimensions: { identity: { content: { summary: '钢厂老板', extra: { keep: true } } }, audience: { content: { summary: '采购经理' } } } })!
const setup = { account_status: 'existing' as const, platform_accounts: [{ platform: 'wechat' as const, account_name: '钢厂老板', profile_url: '' }], answers: { intent: { goals: '获客', direction: '', differentiation: '', strengths: '', resources: '' }, content: { preferences: '', formats: '', tone: '', audience: '', favorites: '', reference_accounts: '' }, boundaries: { exclusions: '', collaboration: '', compliance: '' } }, samples: ['已提供的素材'] }
const project = { ...mockProjects[0], id: 'owner', name: '钢厂老板', platform: 'wechat' as const, profile, profile_setup: setup }
function page() {
  const cache = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={cache}><MemoryRouter initialEntries={['/portraits/owner']}><TooltipProvider><Routes><Route path="/portraits/:projectId" element={<PortraitDetailPage />} /></Routes></TooltipProvider></MemoryRouter></QueryClientProvider>)
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.projects.get).mockResolvedValue({ ...mockProjectDetail, project })
  vi.mocked(api.projects.getAccountProfile).mockResolvedValue(profile)
})
describe('portrait detail', () => {
  it('edits the original portrait with optimistic revision checks and preserves structured fields', async () => {
    vi.mocked(api.projects.updateProfileDimension).mockResolvedValue({ ...profile, version: 4 })
    page()
    fireEvent.click(await screen.findByRole('button', { name: '修改定位' }))
    fireEvent.change(screen.getByLabelText('定位 · 概述'), { target: { value: '精密钢材老板' } })
    fireEvent.click(within(screen.getByRole('region', { name: '定位资料' })).getByRole('button', { name: '保存此维度' }))
    await waitFor(() => expect(api.projects.updateProfileDimension).toHaveBeenCalledWith('owner', 'identity', 3, expect.objectContaining({ content: { summary: '精密钢材老板', extra: { keep: true } } })))
    expect(await screen.findByText('定位已保存')).toBeInTheDocument()
  })
  it('keeps edits after a conflict and does not silently retry against a newer revision', async () => {
    vi.mocked(api.projects.updateProfileDimension).mockRejectedValue(new Error('profile_revision_conflict'))
    page()
    fireEvent.click(await screen.findByRole('button', { name: '修改定位' }))
    fireEvent.change(screen.getByLabelText('定位 · 概述'), { target: { value: '保留我的修改' } })
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue({ ...profile, version: 4 })
    fireEvent.click(within(screen.getByRole('region', { name: '定位资料' })).getByRole('button', { name: '保存此维度' }))
    expect(await screen.findByText(/服务器版本已变化/)).toBeInTheDocument()
    expect(screen.getByLabelText('定位 · 概述')).toHaveValue('保留我的修改')
    expect(api.projects.updateProfileDimension).toHaveBeenCalledTimes(1)
  })
  it('reuses one identity across platform accounts without replacing the portrait or starting analysis', async () => {
    vi.mocked(api.projects.update).mockResolvedValue(project)
    page()
    fireEvent.click(await screen.findByRole('button', { name: '管理平台账号' }))
    fireEvent.click(screen.getByRole('button', { name: '添加账号' }))
    fireEvent.change(screen.getByLabelText('账号 2 平台'), { target: { value: 'seednote' } })
    fireEvent.click(screen.getByRole('button', { name: '保存身份与账号' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('owner', expect.objectContaining({ profile_setup: { ...setup, analyze: false, platform_accounts: [...setup.platform_accounts, { platform: 'seednote', profile_url: '' }] } })))
    expect(api.projects.updateProfileDimension).not.toHaveBeenCalled()
  })
})
