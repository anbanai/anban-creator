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
  it('links project actions into task and plan creation with project context', () => {
    render(<ProjectCard project={project} />)

    expect(screen.getByRole('link', { name: '新建任务' })).toHaveAttribute(
      'href',
      '/tasks?create=true&type=article&project_id=project-1&intent=new',
    )
    expect(screen.getByRole('link', { name: '创建计划' })).toHaveAttribute(
      'href',
      '/plans?create=true&type=article&project_id=project-1&intent=schedule',
    )
    expect(screen.getByRole('button', { name: '查看素材/选题池' })).toBeInTheDocument()
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
})
