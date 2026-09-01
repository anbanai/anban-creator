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

    image_ratio: '',
    max_concurrent_tasks: 1,
    config: { wechat_publish_mode: 'api_confirmed' },
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('ProjectCard', () => {
  const stats: ProjectStats = {
    total_tasks: 10,
    completed_tasks: 8,
    failed_tasks: 2,
    running_tasks: 0,
    pending_tasks: 0,
    success_rate: 0.8,
    last_activity_at: '2026-07-01T00:00:00.000Z',
    unused_topics: 6,
  }

  it('shows only basic project activity', () => {
    render(<ProjectCard project={project()} stats={stats} />)

    expect(screen.getByText('10 个任务')).toBeInTheDocument()
    expect(screen.getByText('8 个已完成')).toBeInTheDocument()
    expect(screen.queryByText('创作配置已就绪')).not.toBeInTheDocument()
    expect(screen.queryByText('还有配置可补齐')).not.toBeInTheDocument()
    expect(screen.queryByText(/成功率/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /创建任务/ })).not.toBeInTheDocument()
  })

  it('shows common actions directly and opens the topic pool', async () => {
    const onEdit = vi.fn()
    const onArchive = vi.fn()
    render(<ProjectCard project={project()} stats={stats} onEdit={onEdit} onArchive={onArchive} />)

    expect(screen.queryByRole('button', { name: '更多项目操作：公众号项目' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑项目：公众号项目' }))
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))

    fireEvent.click(screen.getByRole('button', { name: '归档项目：公众号项目' }))
    expect(onArchive).toHaveBeenCalledWith('project-1')

    fireEvent.click(screen.getByRole('button', { name: '选题池：公众号项目，剩余 6 个' }))
    expect(await screen.findByRole('dialog', { name: '选题池 - 公众号项目' })).toBeInTheDocument()
  })

  it('does not report zero remaining topics before stats are available', () => {
    render(<ProjectCard project={project()} />)

    const topicPoolButton = screen.getByRole('button', { name: '选题池：公众号项目' })
    expect(topicPoolButton).toHaveTextContent('选题池')
    expect(topicPoolButton).not.toHaveTextContent('0')
  })

  it('shows restore instead of archive for archived projects', () => {
    const onArchive = vi.fn()
    const onRestore = vi.fn()
    render(
      <ProjectCard
        project={project({ status: 'archived' })}
        stats={stats}
        onArchive={onArchive}
        onRestore={onRestore}
      />,
    )

    expect(screen.queryByRole('button', { name: '归档项目：公众号项目' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复项目：公众号项目' }))
    expect(onRestore).toHaveBeenCalledWith('project-1')
  })
})
