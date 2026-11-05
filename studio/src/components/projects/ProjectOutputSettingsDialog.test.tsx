import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Project } from '@/types'
import { ProjectOutputSettingsDialog } from './ProjectOutputSettingsDialog'

const updateProject = vi.hoisted(() => vi.fn())

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { is_admin: false } }) }))
vi.mock('@/lib/api', () => ({ api: { projects: { update: updateProject } } }))

describe('ProjectOutputSettingsDialog', () => {
  it('updates the primary platform when changing an ecommerce project to another type', async () => {
    const project = {
      id: 'ecommerce-project',
      platform: 'ecommerce',
      agent_ids: ['ecommerce'],
      name: '电商人设',
    } as Project
    updateProject.mockResolvedValue({ ...project, platform: 'seednote', agent_ids: ['seednote'] })
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(
      <QueryClientProvider client={queryClient}>
        <ProjectOutputSettingsDialog project={project} onClose={vi.fn()} onSaved={vi.fn()} />
      </QueryClientProvider>,
    )

    const dialog = screen.getByRole('dialog', { name: '设置创作类型' })
    fireEvent.click(within(dialog).getByRole('button', { name: '种草笔记' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存并继续创作' }))

    await waitFor(() => expect(updateProject).toHaveBeenCalledWith('ecommerce-project', {
      agent_ids: ['seednote'],
      platform: 'seednote',
    }))
  })
})
