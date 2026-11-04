import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Project } from '@/types'
import { PersonaCreationSettings } from './PersonaCreationSettings'

const updateProject = vi.hoisted(() => vi.fn())

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { is_admin: false } }) }))
vi.mock('@/lib/api', () => ({ api: { projects: { update: updateProject } } }))

function openSettings(platform: Project['platform'], agent_ids?: string[]) {
  const project = { id: 'persona-1', name: '测试人设', platform, agent_ids }
  updateProject.mockImplementation(async (_id, values) => ({ ...project, ...values }))
  const cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={cache}><PersonaCreationSettings project={project} disabled={false} /></QueryClientProvider>)
  fireEvent.click(screen.getByRole('button', { name: '创作类型：测试人设' }))
  return screen.getByRole('dialog')
}

describe('PersonaCreationSettings', () => {
  beforeEach(() => vi.clearAllMocks())

  it.each([['explicit', ['ecommerce']], ['legacy', undefined]] as const)('shows the selected ecommerce type for %s personas', async (_name, agents) => {
    const dialog = openSettings('ecommerce', agents ? [...agents] : undefined)
    expect(within(dialog).getByRole('button', { name: '电商设计' })).toHaveAttribute('aria-pressed', 'true')
    await waitFor(() => expect(within(dialog).getByText('已选 1 项')).toBeVisible())
  })

  it('replaces other types with ecommerce and saves its primary platform', async () => {
    const dialog = openSettings('wechat', ['wechat-article', 'seednote'])
    fireEvent.click(within(dialog).getByRole('button', { name: '电商设计' }))
    expect(within(dialog).getByRole('button', { name: '公众号文章' })).toHaveAttribute('aria-pressed', 'false')
    expect(within(dialog).getByRole('button', { name: '种草笔记' })).toHaveAttribute('aria-pressed', 'false')
    await waitFor(() => expect(within(dialog).getByText('已选 1 项')).toBeVisible())
    fireEvent.click(within(dialog).getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(updateProject).toHaveBeenCalledWith('persona-1', { agent_ids: ['ecommerce'], platform: 'ecommerce' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it.each([
    ['公众号贴图', 'wechat-picture', 'wechat'],
    ['素材混剪', 'local-life', 'local-life'],
  ])('removes ecommerce and saves the matching platform when selecting %s', async (label, agent, platform) => {
    const dialog = openSettings('ecommerce', ['ecommerce'])
    fireEvent.click(within(dialog).getByRole('button', { name: label }))
    expect(within(dialog).getByRole('button', { name: '电商设计' })).toHaveAttribute('aria-pressed', 'false')
    await waitFor(() => expect(within(dialog).getByText('已选 1 项')).toBeVisible())
    fireEvent.click(within(dialog).getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(updateProject).toHaveBeenCalledWith('persona-1', { agent_ids: [agent], platform }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
