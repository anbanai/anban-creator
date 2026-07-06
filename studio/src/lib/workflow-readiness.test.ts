import { describe, expect, it } from 'vitest'
import { parseWorkflowStatus, workflowReadinessLabel } from './workflow-readiness'
import type { WorkflowStatus } from '@/types'

const workflow: WorkflowStatus = {
  version: 'creation_workflow_v1',
  current_stage: 'review',
  stages: [{ key: 'review', label: '质量复盘', status: 'completed' }],
  review: {
    overall_score: 72,
    readiness: 'ready_with_minor_edits',
    risks: ['标题偏弱'],
    next_actions: ['改标题'],
    strengths: ['结构完整'],
  },
}

describe('workflow readiness helpers', () => {
  it('parses workflow objects and JSON strings', () => {
    expect(parseWorkflowStatus(workflow)?.current_stage).toBe('review')
    expect(parseWorkflowStatus(JSON.stringify(workflow))?.review?.overall_score).toBe(72)
  })

  it('returns null for invalid workflow strings', () => {
    expect(parseWorkflowStatus('broken-json')).toBeNull()
  })

  it('standardizes review readiness labels', () => {
    expect(workflowReadinessLabel({ ...workflow, review: { ...workflow.review!, readiness: 'ready' } })).toBe('可发布')
    expect(workflowReadinessLabel(workflow)).toBe('建议修改')
    expect(workflowReadinessLabel({ ...workflow, review: { ...workflow.review!, readiness: 'needs_revision' } })).toBe('需重做')
    expect(workflowReadinessLabel({ ...workflow, review: null })).toBe('')
  })
})
