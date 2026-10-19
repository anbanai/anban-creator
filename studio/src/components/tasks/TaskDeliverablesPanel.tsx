import { AlertCircle, CheckCircle2, CircleDashed, FolderOpen, LoaderCircle } from 'lucide-react'
import type { TaskFile, TaskStatus } from '@/types'
import { Button } from '@/components/common/button'
import { TaskFileDownloads, TaskStageFiles } from './TaskStageFiles'

export interface TaskDeliverablesPanelProps {
  taskId: string
  taskStatus: TaskStatus
  files: TaskFile[]
  taskType?: string
  filesLoading?: boolean
  filesError?: boolean
  onRetryFiles?: () => void
}

function emptyMessage(status: TaskStatus) {
  if (status === 'running' || status === 'pending') return '作品完成后，交付文件会显示在这里。'
  if (status === 'failed') return '本次执行没有留下可交付文件。'
  if (status === 'cancelled') return '作品已取消，当前没有可交付文件。'
  return '当前作品没有可交付文件。'
}

export function TaskDeliverablesPanel({ taskId, taskStatus, files, taskType, filesLoading = false, filesError = false, onRetryFiles }: TaskDeliverablesPanelProps) {
  const visibleFiles = files.filter((file) => file.state === 'delivered' || file.state === 'retained')
  const hasFiles = visibleFiles.length > 0
  const hasDeliverables = visibleFiles.some((file) => file.state === 'delivered' && file.is_deliverable)
  const Icon = hasFiles ? CheckCircle2 : CircleDashed

  return (
    <section aria-label="交付结果" className="rounded-lg border border-border/70 bg-card p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className={`flex size-8 shrink-0 items-center justify-center rounded-full ${hasFiles ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'bg-muted text-muted-foreground'}`}>
            <Icon className="size-4" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h2 className="text-base font-semibold">交付结果</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              {hasDeliverables ? '可直接预览或下载最终交付文件。' : hasFiles ? '当前有已保留或阶段文件，可按需查看。' : '交付文件会显示在这里，支持预览或下载。'}
            </p>
          </div>
        </div>
        {hasFiles ? <TaskFileDownloads taskId={taskId} files={visibleFiles} /> : null}
      </div>
      {hasFiles ? (
        <div className="mt-4 border-t border-border/70 pt-4">
          <TaskStageFiles taskId={taskId} taskType={taskType} files={visibleFiles} />
        </div>
      ) : filesError ? (
        <div role="alert" className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-border/70 pt-4 text-sm text-destructive">
          <span className="inline-flex items-center gap-2"><AlertCircle className="size-4" aria-hidden="true" />暂时无法确认是否有交付文件。</span>
          {onRetryFiles ? <Button type="button" size="sm" variant="outline" onClick={onRetryFiles}>重试</Button> : null}
        </div>
      ) : filesLoading ? (
        <div role="status" className="mt-4 flex items-center gap-2 border-t border-border/70 pt-4 text-sm text-muted-foreground">
          <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />正在读取交付文件…
        </div>
      ) : (
        <div className="mt-4 flex items-center gap-2 border-t border-border/70 pt-4 text-sm text-muted-foreground">
          <FolderOpen className="size-4" aria-hidden="true" />
          {emptyMessage(taskStatus)}
        </div>
      )}
    </section>
  )
}
