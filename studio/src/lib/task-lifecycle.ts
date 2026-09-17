import type { Task, TaskLifecycle, TaskLifecycleStage } from '@/types'

const lifecycleStateLabels: Record<TaskLifecycleStage['state'], string> = {
  pending: '待执行',
  active: '进行中',
  complete: '已完成',
  blocked: '需要处理',
  failed: '失败',
  cancelled: '已取消',
  skipped: '已跳过',
}

export function lifecycleStageStateLabel(state: TaskLifecycleStage['state']): string {
  return lifecycleStateLabels[state]
}

export function shouldApplyLifecycleRevision(currentRevision: number, nextRevision: number): boolean {
  return Number.isFinite(nextRevision) && nextRevision > currentRevision
}

export function currentLifecycleStage(lifecycle?: TaskLifecycle): TaskLifecycleStage | null {
  if (!Array.isArray(lifecycle?.stages) || lifecycle.stages.length === 0) return null
  return lifecycle.stages.find((stage) => stage.state === 'active')
    ?? lifecycle.stages.find((stage) => stage.state === 'blocked' || stage.state === 'failed')
    ?? lifecycle.stages.find((stage) => stage.state === 'cancelled')
    ?? lifecycle.stages.find((stage) => stage.state === 'pending')
    ?? lifecycle.stages[lifecycle.stages.length - 1]
    ?? null
}

export function shouldStreamTaskLifecycle(task: Pick<Task, 'status' | 'lifecycle'>): boolean {
  if (task.status === 'running') return true
  if (task.status !== 'completed' || !Array.isArray(task.lifecycle?.stages)) return false
  return task.lifecycle.stages.some((stage) => stage.source === 'server'
    && (stage.state === 'pending' || stage.state === 'active' || stage.state === 'blocked'))
}

export function taskStageSummary(task: Pick<Task, 'status' | 'lifecycle'>): { title: string; state?: TaskLifecycleStage['state'] } {
  const stages = Array.isArray(task.lifecycle?.stages) ? task.lifecycle.stages : []
  if (task.status === 'failed' || task.status === 'cancelled') {
    const terminalState = task.status === 'failed' ? 'failed' : 'cancelled'
    const recoveryStage = stages.find((stage) => stage.kind === 'work' && stage.state === terminalState)
      ?? stages.find((stage) => stage.kind === 'work' && stage.state === 'active')
      ?? stages.find((stage) => stage.kind === 'work' && stage.state === 'pending')
      ?? [...stages].reverse().find((stage) => stage.kind === 'work')
    if (recoveryStage) return { title: recoveryStage.title, state: recoveryStage.state }
  }
  const stage = currentLifecycleStage(task.lifecycle)
  if (stage) return { title: stage.title, state: stage.state }
  if (task.status === 'running') return { title: '正在制定执行计划', state: 'active' }
  if (task.status === 'pending') return { title: '等待执行', state: 'pending' }
  if (task.status === 'completed') return { title: '任务已完成', state: 'complete' }
  if (task.status === 'failed') return { title: '任务失败', state: 'failed' }
  return { title: '任务已取消', state: 'cancelled' }
}
