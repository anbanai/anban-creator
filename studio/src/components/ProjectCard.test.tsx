import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project, ProjectStats } from '@/types'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '写实',
    writer: '犀利',
    theme: '简洁',
    author: 'Anban',
    template_id: '',
    reference_image_url: '',
    image_ratio: '',
    max_concurrent_tasks: 1,
    config: { enable_publishing: true, require_publish_approval: true },
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('ProjectCard', () => {
  it('shows a compact readiness summary and primary create action', () => {
    const onCreateTask = vi.fn()
    const stats: ProjectStats = {
      total_tasks: 10,
      completed_tasks: 8,
      failed_tasks: 2,
      running_tasks: 0,
      pending_tasks: 0,
      success_rate: 0.8,
      last_activity_at: '2026-07-01T00:00:00.000Z',
    }

    render(<ProjectCard project={project()} stats={stats} onCreateTask={onCreateTask} />)

    expect(screen.getByText('创作配置已就绪')).toBeInTheDocument()
    expect(screen.getByText('发布需审核')).toBeInTheDocument()
    expect(screen.getByText('视觉已配置')).toBeInTheDocument()
    expect(screen.getByText('写作已配置')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '用此项目创建任务' }))
    expect(onCreateTask).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
  })
})
