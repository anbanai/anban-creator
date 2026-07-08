import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { WorkflowReviewSummary } from './TaskWorkflowPanel'
import type { WorkflowStatus } from '@/types'

const workflow: WorkflowStatus = {
  version: 'creation_workflow_v1',
  current_stage: 'draft',
  stages: [
    { key: 'draft', label: '初稿', status: 'completed', artifact_paths: ['03-draft.md'] },
    { key: 'html', label: '排版 HTML', status: 'running' },
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
  it('renders review summary with readiness risks and next actions first', () => {
    render(<WorkflowReviewSummary workflow={workflow} />)

    expect(screen.getByText('发布前检查')).toBeInTheDocument()
    expect(screen.getByText('质量复盘')).toBeInTheDocument()
    expect(screen.getByText('91')).toBeInTheDocument()
    expect(screen.getByText('可发布')).toBeInTheDocument()
    expect(screen.getByText('风险')).toBeInTheDocument()
    expect(screen.getByText('标题还可以更具体')).toBeInTheDocument()
    expect(screen.getByText('下一步')).toBeInTheDocument()
    expect(screen.getByText('发布前微调标题')).toBeInTheDocument()
    expect(screen.getByText('优势')).toBeInTheDocument()
    expect(screen.getByText('草稿发布失败')).toBeInTheDocument()
  })

  it('renders nothing when review is missing', () => {
    const { container } = render(
      <WorkflowReviewSummary workflow={{ ...workflow, review: null }} />,
    )

    expect(container.firstChild).toBeNull()
  })
})
