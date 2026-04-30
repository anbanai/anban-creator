import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import TaskWorkflowPanel from './TaskWorkflowPanel'
import type { WorkflowStatus } from '@/types'

const workflow: WorkflowStatus = {
  version: 'creation_workflow_v1',
  current_stage: 'review',
  stages: [
    { key: 'draft', label: '初稿', status: 'completed', artifact_paths: ['03-draft.md'] },
    { key: 'review', label: '质量复盘', status: 'completed', artifact_paths: ['review.json'] },
  ],
  warnings: [{ code: 'publish_failed', message: '草稿发布失败' }],
  review: {
    overall_score: 91,
    readiness: 'ready',
    strengths: ['账号匹配'],
    risks: ['标题还可以更具体'],
    next_actions: ['发布前微调标题'],
  },
}

describe('TaskWorkflowPanel', () => {
  it('renders workflow stages and review summary', () => {
    render(<TaskWorkflowPanel workflow={workflow} />)

    expect(screen.getByText('创作流程')).toBeInTheDocument()
    expect(screen.getByText('初稿')).toBeInTheDocument()
    expect(screen.getByText('质量复盘')).toBeInTheDocument()
    expect(screen.getByText('91')).toBeInTheDocument()
    expect(screen.getByText('可发布')).toBeInTheDocument()
    expect(screen.getByText('账号匹配')).toBeInTheDocument()
    expect(screen.getByText('草稿发布失败')).toBeInTheDocument()
  })

  it('renders nothing when workflow is missing', () => {
    const { container } = render(<TaskWorkflowPanel workflow={undefined} />)

    expect(container.firstChild).toBeNull()
  })
})
