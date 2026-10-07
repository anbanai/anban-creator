import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project, ProjectStats } from '@/types'
import { api } from '@/lib/api'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'wechat',
    agent_ids: ['wechat-article'],
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
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('ProjectCard', () => {
  beforeEach(() => {
    vi.spyOn(api.projects, 'feedback').mockResolvedValue({
      project_id: 'project-1', platform: 'wechat', timezone: 'Asia/Shanghai', feedback_paused: false,
      analytics: { revision: 3, status: 'ready', content_count: 6, valid_observation_count: 5 },
      queue: { counts: { queued: 1, running: 0, succeeded: 2, failed: 0, skipped: 1, blocked: 0 }, last_success_at: null },
      next_runs: {}, strategy: { id: '', revision: 0, status: 'unavailable' },
    })
    vi.spyOn(api.projects, 'setFeedbackPaused').mockResolvedValue({ project_id: 'project-1', feedback_paused: true })
    vi.spyOn(api.projects, 'rerunFeedback').mockResolvedValue({ created: 1, enqueued: 1, skipped: 0 })
  })

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

  it('shows project identity with a neutral project mark and Agent icon', () => {
    render(<ProjectCard project={project({ name: '共享品牌', avatar_url: 'https://example.com/avatar.png', keywords: '科技, 内容' })} />)
    expect(screen.getByText('共享品牌')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByText('科技', { exact: true })).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
  })

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
    expect(await screen.findByRole('dialog', { name: /选题池 - 公众号项目/ })).toBeInTheDocument()
  })

  it('does not report zero remaining topics before stats are available', () => {
    render(<ProjectCard project={project()} />)

    const topicPoolButton = screen.getByRole('button', { name: '选题池：公众号项目' })
    expect(topicPoolButton).toHaveTextContent('选题池')
    expect(topicPoolButton).not.toHaveTextContent('0')
  })

  it.each(['', 'wechat', 'seednote', 'moments'] as const)('shows the project profile action for %s projects', (platform) => {
    const onProfile = vi.fn()
    render(<ProjectCard project={project({ platform })} onProfile={onProfile} />)

    fireEvent.click(screen.getByRole('button', { name: '项目画像：公众号项目' }))
    expect(onProfile).toHaveBeenCalledWith(expect.objectContaining({ platform }))
    expect(screen.queryByRole('button', { name: /配置账号画像/ })).not.toBeInTheDocument()
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

  it('shows feedback controls for a neutral project', async () => {
    render(<ProjectCard project={project({ platform: '' })} />)
    expect(await screen.findByText('反馈闭环')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /暂停/ })).toBeEnabled()
  })

  it('pauses feedback scheduling and submits an explicit monthly period', async () => {
    render(<ProjectCard project={project()} />)

    await screen.findByText('反馈闭环')
    fireEvent.click(screen.getByRole('button', { name: /暂停/ }))
    await waitFor(() => expect(api.projects.setFeedbackPaused).toHaveBeenCalledWith('project-1', true))

    fireEvent.click(screen.getByRole('button', { name: '月' }))
    fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-08-01' } })
    fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-08-31' } })
    fireEvent.click(screen.getByRole('button', { name: '检查并排队' }))

    await waitFor(() => expect(api.projects.rerunFeedback).toHaveBeenCalledWith('project-1', 'monthly', { start: '2026-08-01', end: '2026-08-31' }))
  })
})
