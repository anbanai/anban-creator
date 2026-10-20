import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project, ProjectStats } from '@/types'
import { api } from '@/lib/api'
import { projectsApi } from '@/lib/api/projects'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'wechat',
    agent_ids: ['wechat-article'],
    name: '公众号创作项目',
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
    vi.clearAllMocks()
    vi.spyOn(projectsApi, 'memory').mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.spyOn(projectsApi, 'getAccountProfile').mockResolvedValue({
      schema_version: 1, status: 'confirmed', initialization_status: 'ready', version: 1,
      dimensions: {
        identity: { content: {}, sources: [], evidence: [], missing_fields: [] },
        style: { content: {}, sources: [], evidence: [], missing_fields: [] },
        audience: { content: {}, sources: [], evidence: [], missing_fields: [] },
        platforms: { content: {}, sources: [], evidence: [], missing_fields: [] },
        preferences: { content: {}, sources: [], evidence: [], missing_fields: [] },
        memory: { content: {}, sources: [], evidence: [], missing_fields: [] },
      }, analysis_limits: [], follow_up_questions: [],
    })
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

    expect(screen.getByTestId('project-overview-strip')).toHaveTextContent('10')
    expect(screen.getByTestId('project-overview-strip')).toHaveTextContent('8')
    expect(screen.queryByText('10 个作品')).not.toBeInTheDocument()
    expect(screen.queryByText('8 个已完成')).not.toBeInTheDocument()
    expect(screen.queryByText('创作配置已就绪')).not.toBeInTheDocument()
    expect(screen.queryByText('还有配置可补齐')).not.toBeInTheDocument()
    expect(screen.queryByText(/成功率/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /创建作品/ })).not.toBeInTheDocument()
  })

  it('links directly to the selected persona work list', () => {
    render(<ProjectCard project={project({ id: 'project/one', name: '生活方式人设' })} />)

    expect(screen.getByRole('link', { name: '查看作品：生活方式人设' })).toHaveAttribute('href', '/tasks?project_id=project%2Fone')
  })

  it('does not expose WeChat platform settings for non-WeChat projects', () => {
    render(<ProjectCard project={project({ platform: 'seednote', agent_ids: ['seednote'] })} onChannelConfig={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /平台设置/ })).not.toBeInTheDocument()
  })

  it.each(['moments', 'ecommerce'] as const)('keeps legacy %s projects in their basic settings flow', (platform) => {
    render(<ProjectCard project={project({ platform, agent_ids: null, name: '公众号项目' })} onEdit={vi.fn()} />)
    expect(screen.queryByRole('button', { name: '人设画像：公众号项目' })).not.toBeInTheDocument()
    expect(screen.queryByText('画像待配置')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑项目：公众号项目' })).toBeInTheDocument()
  })

  it('shows a completion ring and compact metric strip from project stats', () => {
    render(<ProjectCard project={project()} stats={stats} />)

    expect(screen.getByRole('progressbar', { name: '作品完成率：80%' })).toBeInTheDocument()
    expect(screen.getByTestId('project-overview-strip')).toHaveTextContent('10')
    expect(screen.getByTestId('project-overview-strip')).toHaveTextContent('8')
    expect(screen.getByTestId('project-overview-strip')).toHaveTextContent('2')
  })

  it('uses the project identity icons instead of repeating creation object labels', () => {
    render(<ProjectCard project={project({
      agent_ids: ['wechat-article', 'hypit', 'whiteboard-animation'],
      profile: {
        schema_version: 1, status: 'confirmed', initialization_status: 'running', version: 4,
        application_source: 'system_analysis', dimensions: {
          identity: { content: {}, sources: [], evidence: [], missing_fields: [] },
          style: { content: {}, sources: [], evidence: [], missing_fields: [] },
          audience: { content: {}, sources: [], evidence: [], missing_fields: [] },
          platforms: { content: {}, sources: [], evidence: [], missing_fields: [] },
          preferences: { content: {}, sources: [], evidence: [], missing_fields: [] },
          memory: { content: {}, sources: [], evidence: [], missing_fields: [] },
        }, analysis_limits: [], follow_up_questions: [],
      },
    })} />)

    expect(screen.getByRole('img', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '视频复刻' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '白板动画' })).toBeInTheDocument()
    expect(screen.queryByLabelText('创作对象')).not.toBeInTheDocument()
    expect(screen.queryByText('公众号文章', { exact: true })).not.toBeInTheDocument()
    expect(screen.getByText('画像分析中')).toBeInTheDocument()
  })

  it('does not show a precise completion percentage when a project has no tasks', () => {
    render(<ProjectCard project={project()} stats={{ ...stats, total_tasks: 0, completed_tasks: 0, failed_tasks: 0 }} />)

    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(screen.getByText('暂无作品')).toBeInTheDocument()
  })

  it('shows common actions directly and opens the topic pool', async () => {
    const onEdit = vi.fn()
    const onArchive = vi.fn()
    render(<ProjectCard project={project()} stats={stats} onEdit={onEdit} onArchive={onArchive} />)

    expect(screen.queryByRole('button', { name: '更多项目操作：公众号创作项目' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑项目：公众号创作项目' }))
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))

    fireEvent.click(screen.getByRole('button', { name: '归档项目：公众号创作项目' }))
    expect(onArchive).toHaveBeenCalledWith('project-1')

    fireEvent.click(screen.getByRole('button', { name: '选题资产：公众号创作项目，待研究 6 个' }))
    expect(await screen.findByRole('dialog', { name: /选题资产 - 公众号创作项目/ })).toBeInTheDocument()
  })

  it('lists project actions directly without a more menu and uses accessible icon targets', async () => {
    const onChannelConfig = vi.fn()
    const onEdit = vi.fn()
    const onUpdateProfile = vi.fn()
    render(<ProjectCard project={project()} onChannelConfig={onChannelConfig} onEdit={onEdit} onUpdateProfile={onUpdateProfile} onArchive={vi.fn()} />)

    expect(screen.queryByText('更多')).not.toBeInTheDocument()
    const channelConfig = screen.getByRole('button', { name: '平台设置：公众号创作项目' })
    const edit = screen.getByRole('button', { name: '编辑项目：公众号创作项目' })
    const memory = screen.getByRole('button', { name: '项目记忆：公众号创作项目' })
    for (const control of [channelConfig, edit, memory]) expect(control).toHaveClass('size-11')
    expect(screen.queryByRole('button', { name: '人设画像：公众号创作项目' })).not.toBeInTheDocument()

    fireEvent.click(channelConfig)
    expect(onChannelConfig).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
    fireEvent.click(edit)
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
    fireEvent.click(memory)
    expect(await screen.findByRole('dialog', { name: /项目记忆/ })).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '对话更新画像' }))
    expect(onUpdateProfile).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
  })

  it('does not report zero remaining topics before stats are available', () => {
    render(<ProjectCard project={project()} />)

    const topicPoolButton = screen.getByRole('button', { name: '选题资产：公众号创作项目' })
    expect(topicPoolButton.querySelector('svg')).toBeInTheDocument()
    expect(topicPoolButton).not.toHaveTextContent('0')
  })

  it.each(['', 'wechat', 'seednote'] as const)('includes profile controls in project memory for %s projects', async (platform) => {
    const agentIDs = platform === 'seednote' ? ['seednote'] : ['wechat-article']
    render(<ProjectCard project={project({ platform, agent_ids: agentIDs })} />)

    fireEvent.click(screen.getByRole('button', { name: '项目记忆：公众号创作项目' }))
    expect(await screen.findByRole('region', { name: '项目画像' })).toBeInTheDocument()
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

    expect(screen.queryByRole('button', { name: '归档项目：公众号创作项目' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复项目：公众号创作项目' }))
    expect(onRestore).toHaveBeenCalledWith('project-1')
  })

  it('loads feedback on demand and retries only after an explicit action', async () => {
    render(<ProjectCard project={project({ platform: '' })} />)

    expect(api.projects.feedback).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '查看反馈闭环' }))
    expect(await screen.findByText('反馈闭环')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /暂停/ })).toBeEnabled()
    expect(api.projects.feedback).toHaveBeenCalledTimes(1)
  })

  it('does not automatically retry a failed feedback dashboard request', async () => {
    vi.mocked(api.projects.feedback).mockRejectedValue(new Error('server error'))
    render(<ProjectCard project={project()} />)

    fireEvent.click(screen.getByRole('button', { name: '查看反馈闭环' }))
    expect(await screen.findByText('反馈状态暂不可用')).toBeInTheDocument()
    expect(api.projects.feedback).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '重新加载反馈状态' }))
    await waitFor(() => expect(api.projects.feedback).toHaveBeenCalledTimes(2))
  })

  it('pauses feedback scheduling and submits an explicit monthly period', async () => {
    render(<ProjectCard project={project()} />)

    fireEvent.click(screen.getByRole('button', { name: '查看反馈闭环' }))
    await screen.findByRole('button', { name: /暂停/ })
    fireEvent.click(screen.getByRole('button', { name: /暂停/ }))
    await waitFor(() => expect(api.projects.setFeedbackPaused).toHaveBeenCalledWith('project-1', true))

    fireEvent.click(screen.getByRole('button', { name: '月' }))
    fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-08-01' } })
    fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-08-31' } })
    fireEvent.click(screen.getByRole('button', { name: '检查并排队' }))

    await waitFor(() => expect(api.projects.rerunFeedback).toHaveBeenCalledWith('project-1', 'monthly', { start: '2026-08-01', end: '2026-08-31' }))
  })
})
