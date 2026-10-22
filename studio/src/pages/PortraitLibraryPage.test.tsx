import { fireEvent, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { mockProjects } from '@/test/mocks/handlers'
import { normalizeProjectProfile } from '@/lib/project-profile'
import PortraitLibraryPage from './PortraitLibraryPage'

vi.mock('@/lib/api', () => ({ api: { projects: { list: vi.fn() } } }))
beforeEach(() => vi.clearAllMocks())
describe('portrait library', () => {
  it('includes archived portraits and links edits to the original project identity', async () => {
    const profile = normalizeProjectProfile({ dimensions: { identity: { content: { summary: '经营钢材厂', original_certainty: 'stated' } } } })!
    vi.mocked(api.projects.list).mockResolvedValue([
      { ...mockProjects[0], id: 'owner', name: '钢厂老板', platform: 'wechat', status: 'archived', profile },
      { ...mockProjects[0], id: 'sales', name: '钢厂销售', platform: 'wechat', status: 'active' },
    ])
    render(<PortraitLibraryPage />)
    const card = await screen.findByRole('article', { name: '创作身份：钢厂老板' })
    expect(within(card).getByText('已归档')).toBeInTheDocument()
    expect(within(card).getByText('已保存 · 可继续完善')).toBeInTheDocument()
    expect(within(card).getByRole('link', { name: '查看与修改' })).toHaveAttribute('href', '/portraits/owner')
    expect(within(card).getByRole('link', { name: '继续访谈' })).toHaveAttribute('href', '/')
    fireEvent.change(screen.getByLabelText('搜索创作身份'), { target: { value: '销售' } })
    expect(screen.queryByRole('article', { name: '创作身份：钢厂老板' })).not.toBeInTheDocument()
    expect(screen.getByRole('article', { name: '创作身份：钢厂销售' })).toBeInTheDocument()
  })
  it('does not report a read error as an empty library', async () => {
    vi.mocked(api.projects.list).mockRejectedValue(new Error('network unavailable'))
    render(<PortraitLibraryPage />)
    expect(await screen.findByRole('alert')).toHaveTextContent('画像暂时无法读取')
    expect(screen.queryByText('从第一个创作身份开始')).not.toBeInTheDocument()
  })
})
