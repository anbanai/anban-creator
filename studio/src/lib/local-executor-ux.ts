import type { LocalExecutorStatus } from '@/lib/tauri'

const RUNNABLE_STATES = new Set<LocalExecutorStatus['state']>([
  'claiming',
  'running_idle',
  'running_task',
])

export function shouldDefaultRunLocally(status: LocalExecutorStatus | null): boolean {
  return Boolean(status?.running && RUNNABLE_STATES.has(status.state))
}

export function canSubmitLocalTask(status: LocalExecutorStatus | null, optedIn: boolean): boolean {
  return optedIn && shouldDefaultRunLocally(status)
}

export function localExecutorCreateHint(status: LocalExecutorStatus | null): string {
  if (!status) return '正在检测本地执行器状态'
  if (RUNNABLE_STATES.has(status.state) && status.running) {
    return status.current_task_id ? '本地执行器正在运行任务，新任务会排队认领' : '本地执行器运行中，可立即认领'
  }
  if (status.state === 'ready_stopped') return '本地执行器已就绪但未启动，点击启动后本机运行'
  if (status.last_error) return status.last_error
  return status.reason || '本地执行器未就绪，将走云端'
}
