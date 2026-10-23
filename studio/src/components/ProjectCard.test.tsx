import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project, ProjectStats } from '@/types'
import { projectsApi } from '@/lib/api/projects'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'wechat',
    agent_ids: ['wechat-article'],
    name: '公众号人设',
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

  it('offers a whole-card detail link independently of archive actions', () => {
    const onArchive = vi.fn()
    render(<ProjectCard project={project()} onArchive={onArchive} />)
    expect(screen.getByRole('link', { name: '查看人设详情：公众号人设' })).toHaveAttribute('href', '/portraits/project-1')
    fireEvent.click(screen.getByRole('button', { name: '归档人设：公众号人设' }))
    expect(onArchive).toHaveBeenCalledWith('project-1')
  })

  it('shows project identity with a neutral project mark and Agent icon', () => {
    render(<ProjectCard project={project({ name: '共享品牌', avatar_url: 'https://example.com/avatar.png', keywords: '科技, 内容' })} />)
    expect(screen.getByText('共享品牌')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByText('科技', { exact: true })).toBeInTheDocument()
    expect(document.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
  })

  it('omits task totals and feedback controls from the project card', () => {
    render(<ProjectCard project={project()} stats={stats} />)

    expect(screen.queryByTestId('project-overview-strip')).not.toBeInTheDocument()
    expect(screen.queryByText('10 个作品')).not.toBeInTheDocument()
    expect(screen.queryByText('8 个已完成')).not.toBeInTheDocument()
    expect(screen.queryByText('创作配置已就绪')).not.toBeInTheDocument()
    expect(screen.queryByText('还有配置可补齐')).not.toBeInTheDocument()
    expect(screen.queryByText(/成功率/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '查看反馈闭环' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /创建作品/ })).not.toBeInTheDocument()
  })

  it('links directly to the selected persona work list', () => {
    render(<ProjectCard project={project({ id: 'project/one', name: '生活方式人设' })} />)

    expect(screen.getByRole('link', { name: '查看作品：生活方式人设' })).toHaveAttribute('href', '/tasks?project_id=project%2Fone')
  })

  it('does not expose WeChat platform settings for non-WeChat projects', () => {
    render(<ProjectCard project={project({ platform: 'seednote', agent_ids: ['seednote'] })} />)
    expect(screen.queryByRole('button', { name: /平台设置/ })).not.toBeInTheDocument()
  })

  it.each(['moments', 'ecommerce'] as const)('keeps legacy %s projects in their basic settings flow', (platform) => {
    render(<ProjectCard project={project({ platform, agent_ids: null, name: '公众号人设' })} onEdit={vi.fn()} />)
    expect(screen.queryByRole('button', { name: '人设画像：公众号人设' })).not.toBeInTheDocument()
    expect(screen.queryByText('画像待配置')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑人设：公众号人设' })).toBeInTheDocument()
  })

  it('does not render work metrics from project stats', () => {
    render(<ProjectCard project={project()} stats={stats} />)

    expect(screen.queryByRole('progressbar', { name: '作品完成率：80%' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('project-overview-strip')).not.toBeInTheDocument()
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

  it('keeps the card focused on profile content without task statistics', () => {
    render(<ProjectCard project={project({ keywords: '茶知识, 茶文化' })} stats={{ ...stats, total_tasks: 0, completed_tasks: 0, failed_tasks: 0 }} />)

    expect(screen.getByRole('progressbar', { name: '公众号人设画像完善度' })).toBeInTheDocument()
    expect(screen.queryByTestId('project-overview-strip')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无作品')).not.toBeInTheDocument()
    expect(screen.queryByText('反馈闭环')).not.toBeInTheDocument()
    expect(screen.queryByText('尚未添加平台账号')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '继续访谈' })).not.toBeInTheDocument()

    const identity = document.querySelector('[data-slot="project-identity"]') as HTMLElement
    const typeAndTags = identity.querySelector('[data-slot="project-capabilities"]')?.parentElement
    const contentType = typeAndTags?.querySelector('[data-agent-id="wechat-article"]')!
    const keywords = typeAndTags?.querySelector('[data-slot="project-identity-keywords"]')!
    expect(contentType).toHaveAttribute('aria-label', '公众号文章')
    expect(contentType?.compareDocumentPosition(keywords!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(keywords).toHaveTextContent('茶知识茶文化')

    const portraitLink = screen.getByRole('link', { name: '查看画像：公众号人设' })
    expect(portraitLink).toHaveAttribute('href', '/portraits/project-1')
    expect(portraitLink.closest('[aria-label="人设操作：公众号人设"]')).toContainElement(portraitLink)
  })

  it('reserves two lines for every profile summary so progress bars align', () => {
    const short = render(<ProjectCard project={project({ keywords: '' })} />)
    expect(short.container.querySelector('[aria-label="人设画像：公众号人设"] p.line-clamp-2')).toHaveClass('h-10')

    short.unmount()
    const long = render(<ProjectCard project={project({ keywords: '较长的画像摘要内容用于测试卡片高度对齐' })} />)
    expect(long.container.querySelector('[aria-label="人设画像：公众号人设"] p.line-clamp-2')).toHaveClass('h-10')
  })

  it('uses the profile progress bar as the divider before card actions', () => {
    render(<ProjectCard project={project()} />)

    const progress = screen.getByRole('progressbar', { name: '公众号人设画像完善度' })
    const actions = screen.getByLabelText('人设操作：公众号人设')

    expect(progress).toHaveClass('mb-1')
    expect(actions).not.toHaveClass('border-t')
  })

  it('shows common actions directly and opens the topic pool', async () => {
    const onEdit = vi.fn()
    const onArchive = vi.fn()
    render(<ProjectCard project={project()} stats={stats} onEdit={onEdit} onArchive={onArchive} />)

    expect(screen.queryByRole('button', { name: '更多人设操作：公众号人设' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑人设：公众号人设' }))
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))

    fireEvent.click(screen.getByRole('button', { name: '归档人设：公众号人设' }))
    expect(onArchive).toHaveBeenCalledWith('project-1')

    fireEvent.click(screen.getByRole('button', { name: '选题资产：公众号人设，待研究 6 个' }))
    expect(await screen.findByRole('dialog', { name: /选题资产 - 公众号人设/ })).toBeInTheDocument()
  })

  it('keeps project settings under the single edit entry and uses accessible icon targets', async () => {
    const onEdit = vi.fn()
    const onUpdateProfile = vi.fn()
    render(<ProjectCard project={project()} onEdit={onEdit} onUpdateProfile={onUpdateProfile} onArchive={vi.fn()} />)

    expect(screen.queryByText('更多')).not.toBeInTheDocument()
    const edit = screen.getByRole('button', { name: '编辑人设：公众号人设' })
    const memory = screen.getByRole('button', { name: '人设记忆：公众号人设' })
    for (const control of [edit, memory]) expect(control).toHaveClass('size-11')
    expect(screen.queryByRole('button', { name: '平台设置：公众号人设' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '人设画像：公众号人设' })).not.toBeInTheDocument()

    fireEvent.click(edit)
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
    fireEvent.click(memory)
    expect(await screen.findByRole('dialog', { name: /人设记忆/ })).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '对话更新画像' }))
    expect(onUpdateProfile).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
  })

  it('does not report zero remaining topics before stats are available', () => {
    render(<ProjectCard project={project()} />)

    const topicPoolButton = screen.getByRole('button', { name: '选题资产：公众号人设' })
    expect(topicPoolButton.querySelector('svg')).toBeInTheDocument()
    expect(topicPoolButton).not.toHaveTextContent('0')
  })

  it.each(['', 'wechat', 'seednote'] as const)('includes profile controls in project memory for %s projects', async (platform) => {
    const agentIDs = platform === 'seednote' ? ['seednote'] : ['wechat-article']
    render(<ProjectCard project={project({ platform, agent_ids: agentIDs })} />)

    fireEvent.click(screen.getByRole('button', { name: '人设记忆：公众号人设' }))
    expect(await screen.findByRole('region', { name: '人设画像' })).toBeInTheDocument()
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

    expect(screen.queryByRole('button', { name: '归档人设：公众号人设' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消归档人设：公众号人设' }))
    expect(onRestore).toHaveBeenCalledWith('project-1')
  })

  it('places the work list entry in the icon action row', () => {
    render(<ProjectCard project={project()} />)

    const workList = screen.getByRole('link', { name: '查看作品：公众号人设' })
    expect(workList).toBeInTheDocument()
    expect(workList.closest('[aria-label="人设操作：公众号人设"]')).toContainElement(workList)
    expect(screen.queryByRole('button', { name: '查看反馈闭环' })).not.toBeInTheDocument()
  })
})
