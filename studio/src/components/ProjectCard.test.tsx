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
})
