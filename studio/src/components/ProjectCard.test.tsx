import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project } from '@/types'

const project: Project = {
  id: 'project-1',
  user_id: 'user-1',
  platform: 'article',
  name: '公众号项目',
  avatar_url: '',
  profile_url: '',
  keywords: '',
  visual_style: '',
  writer: '',
  theme: '',
  author: '',
  template_id: '',
  reference_image_url: '',
  image_ratio: '',
  max_concurrent_tasks: 1,
  config: {},
  status: 'active',
  created_at: '2026-07-01T00:00:00.000Z',
  updated_at: '2026-07-01T00:00:00.000Z',
}

describe('ProjectCard contextual actions', () => {
  it('keeps project management actions without task or plan shortcuts', () => {
    render(
      <ProjectCard
        project={project}
        onEdit={() => {}}
        onArchive={() => {}}
        onDelete={() => {}}
      />,
    )

    expect(screen.queryByRole('link', { name: '新建任务' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '创建计划' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑项目' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '归档项目' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除项目' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看素材/选题池' })).toBeInTheDocument()
  })

  it('does not offer task or plan shortcuts for moments projects', () => {
    render(<ProjectCard project={{ ...project, platform: 'moments', name: '朋友圈项目' }} />)

    expect(screen.queryByRole('link', { name: '新建任务' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '创建计划' })).not.toBeInTheDocument()
  })

  it('summarizes publishing mode, defaults, and success rate as operating badges', () => {
    render(
      <ProjectCard
        project={{
          ...project,
          visual_style: '',
          writer: 'dan-koe',
          theme: 'autumn-warm',
          author: '安般',
          config: { enable_publishing: true, require_publish_approval: true },
        }}
        stats={{
          total_tasks: 7,
          completed_tasks: 6,
          failed_tasks: 1,
          running_tasks: 0,
          pending_tasks: 0,
          success_rate: 0.86,
          last_activity_at: '2026-07-06T08:00:00.000Z',
        }}
      />,
    )

    expect(screen.getByText('公众号草稿箱')).toBeInTheDocument()
    expect(screen.getByText('发布需审核')).toBeInTheDocument()
    expect(screen.getByText('视觉未配置')).toBeInTheDocument()
    expect(screen.getByText('写作已配置')).toBeInTheDocument()
    expect(screen.getAllByText('成功率 86%').length).toBeGreaterThan(0)
  })

  it('visually quiets archived projects while keeping recovery actions available', () => {
    render(
      <ProjectCard
        project={{ ...project, status: 'archived' }}
        onRestore={() => {}}
      />,
    )

    const card = screen.getByText('公众号项目').closest('.group')
    expect(card).toHaveClass('bg-muted/30', 'opacity-75', 'grayscale-[0.25]')
    expect(card).not.toHaveClass('hover:shadow-md')
    expect(screen.getByText('已归档')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复项目' })).toBeEnabled()
  })
})
