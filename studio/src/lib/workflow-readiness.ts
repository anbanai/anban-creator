import type { WorkflowStatus } from '@/types'

export function parseWorkflowStatus(workflow?: WorkflowStatus | string | null): WorkflowStatus | null {
  if (!workflow) return null
  if (typeof workflow !== 'string') return workflow
  try {
    return JSON.parse(workflow) as WorkflowStatus
  } catch {
    return null
  }
}

export function readinessValueLabel(readiness?: string) {
  switch (readiness) {
    case 'ready':
      return '可发布'
    case 'ready_with_minor_edits':
      return '建议修改'
    case 'needs_revision':
      return '需重做'
    default:
      return readiness || ''
  }
}

export function workflowReadinessLabel(workflow: WorkflowStatus | string | null | undefined) {
  const parsed = parseWorkflowStatus(workflow)
  return readinessValueLabel(parsed?.review?.readiness)
}
