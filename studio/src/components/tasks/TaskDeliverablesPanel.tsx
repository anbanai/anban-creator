import { AlertCircle, CheckCircle2, CircleDashed, FolderOpen, LoaderCircle, ScrollText, Send } from 'lucide-react'
import type { TaskFile, TaskLifecycle, TaskOutcome, TaskStatus, WorkflowStatus } from '@/types'
import { Button } from '@/components/common/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { TaskFileDownloads, TaskStageFiles } from './TaskStageFiles'
import { CompletionSummary, TaskExecutionRail } from './TaskExecutionRail'
import { taskFailurePresentation } from '@/lib/labels'
import { TaskSharePopover } from './TaskSharePopover'

export interface TaskDeliverablesPanelProps {
  taskId: string
  taskStatus: TaskStatus
  files: TaskFile[]
  taskType?: string
  taskKind?: string
  filesLoading?: boolean
  filesError?: boolean
  onRetryFiles?: () => void
  workflow?: WorkflowStatus | string | null
  outcome?: TaskOutcome
  errorMessage?: string
  onResume?: () => void
  isAdmin: boolean
  lifecycle?: TaskLifecycle
  onOpenLogs?: () => void
  deliveryMode?: string
  canFormallyPublish?: boolean
  isNewspic?: boolean
}

function emptyMessage(status: TaskStatus) {
  if (status === 'running' || status === 'pending') return '作品完成后，交付文件会显示在这里。'
  if (status === 'failed') return '本次执行没有留下可交付文件。'
  if (status === 'cancelled') return '作品已取消，当前没有可交付文件。'
  return '当前作品没有可交付文件。'
}

export function TaskDeliverablesPanel({ taskId, taskStatus, files, taskType, taskKind, filesLoading = false, filesError = false, onRetryFiles, workflow, outcome, errorMessage, onResume, isAdmin, lifecycle, onOpenLogs, deliveryMode, canFormallyPublish, isNewspic }: TaskDeliverablesPanelProps) {
  const settledFiles = files.filter((file) => file.state === 'delivered' || file.state === 'retained')
  const coreFiles = settledFiles.filter((file) => file.is_deliverable === true)
  const internalFiles = isAdmin ? settledFiles.filter((file) => file.is_deliverable !== true) : []
  const hasFiles = coreFiles.length > 0
  const shareable = (taskType === 'wechat-article' || taskType === 'seednote') && (!taskKind || taskKind === 'content_generation')
  const hasShareBody = coreFiles.some((file) => file.state === 'delivered' && (taskType === 'wechat-article' ? file.delivery_role === 'html' && file.mime_type === 'text/html' : file.delivery_role === 'final_markdown' && file.mime_type === 'text/markdown'))
  const Icon = hasFiles ? CheckCircle2 : CircleDashed
  const failure = taskFailurePresentation({ error_message: errorMessage, outcome })
  const internalGroups = new Map<string, TaskFile[]>()
  for (const file of internalFiles) {
    const stageID = file.producer_stage_id || 'other'
    internalGroups.set(stageID, [...(internalGroups.get(stageID) ?? []), file])
  }

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
            {hasFiles ? '核心交付文件已整理在这里，可直接预览或下载。' : '本次任务暂未生成核心交付文件。'}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {shareable ? <TaskSharePopover taskId={taskId} canCreate={hasShareBody && !filesLoading && !filesError} /> : null}
          {hasFiles ? <TaskFileDownloads taskId={taskId} files={coreFiles} /> : null}
          {onOpenLogs ? <Tooltip>
            <TooltipTrigger render={<Button type="button" size="icon-md" variant="ghost" className="hover:bg-transparent dark:hover:bg-transparent aria-expanded:bg-transparent focus-visible:border-transparent focus-visible:bg-transparent" onClick={onOpenLogs} aria-label="查看完整日志" />}>
              <ScrollText className="size-4" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent>查看完整日志</TooltipContent>
          </Tooltip> : null}
        </div>
      </div>
      {taskStatus === 'failed' || taskStatus === 'cancelled' ? (
        <div className="mt-4 space-y-3 border-t border-border/70 pt-4" role="status">
          <div className="flex items-start justify-between gap-3 rounded-lg border border-destructive/15 bg-destructive/5 px-3 py-2.5 text-sm">
            <div className="min-w-0">
              <p className="font-medium text-destructive">{failure?.title || (taskStatus === 'failed' ? '执行失败' : '执行已取消')}</p>
              {failure?.message ? <p className="mt-1 break-words text-xs leading-relaxed text-foreground/80">{failure.message}</p> : null}
              {failure?.recovery ? <p className="mt-1 text-xs text-muted-foreground">{failure.recovery}</p> : null}
              {!failure && taskStatus === 'failed' ? <p className="mt-1 text-xs text-muted-foreground">服务端没有返回失败详情，可继续执行并补充说明。</p> : null}
            </div>
            {onResume ? <Button type="button" size="icon-md" variant="ghost" onClick={onResume} aria-label="继续执行"><Send className="size-4" /></Button> : null}
          </div>
        </div>
      ) : null}
      {hasFiles ? (
        <div className="mt-4 border-t border-border/70 pt-4">
          <TaskStageFiles taskId={taskId} taskType={taskType} files={coreFiles} />
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
      {taskStatus === 'completed' ? <CompletionSummary workflow={workflow} outcome={outcome} /> : null}
      {taskStatus === 'completed' && outcome?.warnings.length ? (
        <div className="mt-3 space-y-1 border-t border-border/70 pt-3" aria-label="交付提醒">
          {outcome.warnings.map((warning, index) => <p key={`${warning.code}-${index}`} className="text-xs leading-relaxed text-amber-700 dark:text-amber-400">{warning.message}</p>)}
        </div>
      ) : null}
      {internalFiles.length > 0 ? (
        <details className="mt-4 border-t border-border/70 pt-4">
          <summary className="cursor-pointer text-sm font-medium text-muted-foreground">内部文件 ({internalFiles.length})</summary>
          <div className="mt-3 space-y-4">
            <p className="text-xs text-muted-foreground">仅 Admin 可预览，用于检查执行产物；内部文件不提供下载。</p>
            {Array.from(internalGroups, ([stageID, groupedFiles]) => {
              const title = lifecycle?.stages.find((stage) => stage.id === stageID)?.title || (stageID === 'other' ? '其他文件' : stageID)
              return <section key={stageID} aria-label={title} className="space-y-2"><h3 className="text-xs font-medium text-foreground">{title}</h3><TaskStageFiles taskId={taskId} taskType={taskType} files={groupedFiles} /></section>
            })}
          </div>
        </details>
      ) : null}
      {lifecycle?.stages.some((stage) => stage.source === 'server') && onOpenLogs ? (
        <div className="mt-4 border-t border-border/70 pt-4">
          <TaskExecutionRail
            taskId={taskId}
            status={taskStatus}
            lifecycle={lifecycle}
            deliveryMode={deliveryMode}
            canFormallyPublish={canFormallyPublish}
            isNewspic={isNewspic}
            workflow={workflow}
            outcome={outcome}
            errorMessage={errorMessage}
            onOpenLogs={onOpenLogs}
            showFailureSummary={false}
            showCompletionSummary={false}
            showFiles={false}
            resultMode
          />
        </div>
      ) : null}
    </section>
  )
}
