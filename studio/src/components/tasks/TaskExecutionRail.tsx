import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleDashed,
  Clock3,
  ExternalLink,
  FileCheck2,
  ListTree,
  LoaderCircle,
  Minus,
  PauseCircle,
  RefreshCw,
  ScrollText,
  Send,
  XCircle,
  type LucideIcon,
} from 'lucide-react'

import { api } from '@/lib/api'
import { taskFailurePresentation } from '@/lib/labels'
import { parseWorkflowStatus, readinessValueLabel } from '@/lib/workflow-readiness'
import { cn } from '@/lib/utils'
import type {
  TaskLifecycle,
  TaskLifecycleStage,
  TaskLifecycleStageState,
  TaskOutcome,
  TaskStatus,
  WechatPublication,
  WorkflowStatus,
} from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/common/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

const lifecycleStatePresentation: Record<TaskLifecycleStageState, {
  label: string
  icon: LucideIcon
  iconClassName: string
  textClassName: string
}> = {
  pending: { label: '待执行', icon: Circle, iconClassName: 'border-border bg-background text-muted-foreground', textClassName: 'text-muted-foreground' },
  active: { label: '进行中', icon: LoaderCircle, iconClassName: 'border-primary/30 bg-primary/10 text-primary', textClassName: 'text-primary' },
  complete: { label: '已完成', icon: Check, iconClassName: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400', textClassName: 'text-emerald-700 dark:text-emerald-400' },
  blocked: { label: '需要处理', icon: PauseCircle, iconClassName: 'border-amber-500/35 bg-amber-500/10 text-amber-700 dark:text-amber-400', textClassName: 'text-amber-700 dark:text-amber-400' },
  failed: { label: '失败', icon: XCircle, iconClassName: 'border-destructive/30 bg-destructive/10 text-destructive', textClassName: 'text-destructive' },
  cancelled: { label: '已取消', icon: Ban, iconClassName: 'border-border bg-muted text-muted-foreground', textClassName: 'text-muted-foreground' },
  skipped: { label: '已跳过', icon: Minus, iconClassName: 'border-border bg-muted text-muted-foreground', textClassName: 'text-muted-foreground' },
}

const terminalPublicationStatuses = new Set(['published', 'publish_failed', 'unsupported', 'awaiting_manual_publish'])

function formatStageTime(value?: string) {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(date)
}

function currentStageIndex(stages: TaskLifecycleStage[], status: TaskStatus) {
  if (status === 'failed' || status === 'cancelled') {
    const terminalState = status === 'failed' ? 'failed' : 'cancelled'
    const terminalIndex = stages.findIndex((stage) => stage.kind === 'work' && stage.state === terminalState)
    if (terminalIndex >= 0) return terminalIndex
    const activeWorkIndex = stages.findIndex((stage) => stage.kind === 'work' && stage.state === 'active')
    if (activeWorkIndex >= 0) return activeWorkIndex
    const pendingWorkIndex = stages.findIndex((stage) => stage.kind === 'work' && stage.state === 'pending')
    if (pendingWorkIndex >= 0) return pendingWorkIndex
    for (let index = stages.length - 1; index >= 0; index -= 1) {
      if (stages[index].kind === 'work') return index
    }
  }
  const activeIndex = stages.findIndex((stage) => stage.state === 'active')
  if (activeIndex >= 0) return activeIndex
  const attentionIndex = stages.findIndex((stage) => stage.state === 'blocked' || stage.state === 'failed')
  if (attentionIndex >= 0) return attentionIndex
  const cancelledIndex = stages.findIndex((stage) => stage.state === 'cancelled')
  if (cancelledIndex >= 0) return cancelledIndex
  const pendingIndex = stages.findIndex((stage) => stage.state === 'pending')
  return pendingIndex >= 0 ? pendingIndex : Math.max(0, stages.length - 1)
}

function automaticallyExpandedStageIds(
  stages: TaskLifecycleStage[],
  status: TaskStatus,
  currentStage?: TaskLifecycleStage,
) {
  const expanded = stages
    .filter((stage) => stage.state === 'active' || stage.state === 'blocked' || stage.state === 'failed' || stage.state === 'cancelled')
    .map((stage) => stage.id)
  if ((status === 'failed' || status === 'cancelled') && currentStage?.kind === 'work') {
    expanded.push(currentStage.id)
  }
  return new Set(expanded)
}

function hasSubmissionEvidence(publication?: WechatPublication) {
  return Boolean(
    publication?.submit_attempted_at
      || publication?.publish_id
      || publication?.msg_data_id
      || publication?.msg_id,
  )
}

export interface TaskExecutionRailProps {
  taskId: string
  status: TaskStatus
  lifecycle?: TaskLifecycle
  workflow?: WorkflowStatus | string | null
  outcome?: TaskOutcome
  errorMessage?: string
  onOpenLogs: () => void
  onResume?: () => void
}

export function TaskExecutionRail({
  taskId,
  status,
  lifecycle,
  workflow,
  outcome,
  errorMessage,
  onOpenLogs,
  onResume,
}: TaskExecutionRailProps) {
  const queryClient = useQueryClient()
  const stages = lifecycle?.stages ?? []
  const [confirmPublish, setConfirmPublish] = useState(false)
  const hasServerPublicationStages = stages.some((stage) => stage.source === 'server')
  const publicationStageSignature = stages
    .filter((stage) => stage.source === 'server')
    .map((stage) => `${stage.id}:${stage.state}:${stage.latest_update ?? ''}`)
    .join('|')
  const previousPublicationStageSignature = useRef(publicationStageSignature)
  const lastWorkStageId = [...stages].reverse().find((stage) => stage.kind === 'work')?.id
  const currentIndex = currentStageIndex(stages, status)
  const currentStage = stages[currentIndex]
  const failure = taskFailurePresentation({ error_message: errorMessage })

  const [expandedStages, setExpandedStages] = useState<Set<string>>(() => automaticallyExpandedStageIds(stages, status, currentStage))

  useEffect(() => {
    setExpandedStages(automaticallyExpandedStageIds(stages, status, currentStage))
  }, [currentStage?.id, lifecycle?.revision, status])

  const publicationQuery = useQuery({
    queryKey: ['wechat-publication', taskId],
    queryFn: () => api.tasks.getWechatPublication(taskId),
    enabled: hasServerPublicationStages,
    retry: false,
    refetchInterval: (query) => {
      const publicationStatus = query.state.data?.status
      return publicationStatus && !terminalPublicationStatuses.has(publicationStatus) && publicationStatus !== 'drafted' ? 5_000 : false
    },
  })
  const publication = publicationQuery.data
  const refetchPublication = publicationQuery.refetch

  useEffect(() => {
    if (previousPublicationStageSignature.current === publicationStageSignature) return
    previousPublicationStageSignature.current = publicationStageSignature
    if (hasServerPublicationStages) {
      void refetchPublication()
    }
  }, [hasServerPublicationStages, publicationStageSignature, refetchPublication])

  const invalidatePublication = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ['wechat-publication', taskId] }),
    queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
  ])
  const reconcile = useMutation({ mutationFn: () => api.tasks.reconcileWechat(taskId), onSuccess: invalidatePublication })
  const publish = useMutation({
    mutationFn: () => api.tasks.publishWechat(taskId),
    onSuccess: () => {
      setConfirmPublish(false)
      return invalidatePublication()
    },
  })
  const retryPublish = useMutation({ mutationFn: () => api.tasks.retryWechatPublish(taskId), onSuccess: invalidatePublication })
  const recoverDraft = useMutation({ mutationFn: () => api.tasks.recoverWechatPublication(taskId), onSuccess: invalidatePublication })
  const selectArticle = useMutation({
    mutationFn: (articleId: string) => api.tasks.selectWechatArticle(taskId, articleId),
    onSuccess: invalidatePublication,
  })
  const publicationActionPending = reconcile.isPending || publish.isPending || retryPublish.isPending || recoverDraft.isPending || selectArticle.isPending
  const publicationActionFailed = reconcile.isError || publish.isError || retryPublish.isError || recoverDraft.isError || selectArticle.isError

  const announcement = currentStage
    ? `${currentStage.title}，${lifecycleStatePresentation[currentStage.state].label}`
    : status === 'running' ? '正在制定执行计划' : ''

  if (stages.length === 0) {
    const emptyState = status === 'running'
      ? { title: '正在制定执行计划', description: 'Agent 声明阶段后会在这里持续更新', icon: CircleDashed, iconClassName: 'border-primary/30 bg-primary/10 text-primary' }
      : status === 'failed'
        ? { title: failure?.title || '任务未完成', description: failure?.message || '服务端没有返回失败详情，可继续执行并补充说明。', icon: XCircle, iconClassName: 'border-destructive/30 bg-destructive/10 text-destructive' }
        : status === 'cancelled'
          ? { title: '执行已停止', description: '任务已取消，可从已有上下文继续。', icon: Ban, iconClassName: 'border-border bg-muted text-muted-foreground' }
          : status === 'completed'
            ? { title: '任务已完成', description: '执行结果已经保存', icon: Check, iconClassName: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' }
            : { title: '等待任务开始', description: '启动后会在这里显示执行阶段', icon: Clock3, iconClassName: 'border-border bg-background text-muted-foreground' }
    const EmptyStateIcon = emptyState.icon
    return (
      <section className="border-y border-border py-4" aria-labelledby="task-execution-heading">
        <RailHeading onOpenLogs={onOpenLogs} />
        <div className="mx-4 mt-4 flex min-h-14 items-start gap-3 border-l border-border pl-5">
          <span className={cn('flex size-7 shrink-0 items-center justify-center rounded-full border', emptyState.iconClassName)}>
            <EmptyStateIcon className={cn('size-4', status === 'running' && 'animate-spin')} />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-foreground">{emptyState.title}</p>
            <p className="mt-0.5 text-xs text-muted-foreground">{emptyState.description}</p>
            {status === 'failed' && failure?.recovery && <p className="mt-1 text-xs text-muted-foreground">{failure.recovery}</p>}
            {(status === 'failed' || status === 'cancelled') && onResume && (
              <Button size="xs" variant="outline" className="mt-3" onClick={onResume}>
                <Send className="size-3" />
                继续执行
              </Button>
            )}
          </div>
        </div>
      </section>
    )
  }

  return (
    <section className="border-y border-border py-4" aria-labelledby="task-execution-heading">
      <div className="sr-only" aria-live="polite" aria-atomic="true">{announcement}</div>
      <RailHeading onOpenLogs={onOpenLogs} />
      <ol className="mx-4 mt-3" aria-label="任务执行阶段">
        {stages.map((stage, index) => {
          const presentation = lifecycleStatePresentation[stage.state]
          const Icon = presentation.icon
          const isExpanded = expandedStages.has(stage.id)
          const canExpand = stage.state !== 'pending' && Boolean(
            stage.goal || stage.latest_update || stage.id === lastWorkStageId || stage.source === 'server',
          )
          const isCurrent = index === currentIndex
          const isTerminalRecoveryPoint = isCurrent && stage.kind === 'work' && (status === 'failed' || status === 'cancelled')
          const time = formatStageTime(stage.completed_at || stage.started_at)
          return (
            <li key={stage.id} className="relative grid grid-cols-[28px_minmax(0,1fr)] gap-3 pb-1 last:pb-0">
              {index < stages.length - 1 && <span aria-hidden="true" className="absolute left-[13px] top-7 h-[calc(100%-12px)] w-px bg-border" />}
              <span className={cn('relative z-10 mt-2 flex size-7 items-center justify-center rounded-full border', presentation.iconClassName)}>
                <Icon className={cn('size-3.5', stage.state === 'active' && 'animate-spin')} />
              </span>
              <div className={cn('min-w-0 border-b border-border/70 py-2.5 last:border-b-0', isCurrent && 'bg-muted/20 -mx-2 px-2')}>
                <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <button
                    type="button"
                    aria-label={`${stage.title}，${presentation.label}`}
                    aria-current={isCurrent ? 'step' : undefined}
                    aria-expanded={canExpand ? isExpanded : undefined}
                    disabled={!canExpand}
                    onClick={() => {
                      if (!canExpand) return
                      setExpandedStages((previous) => {
                        const next = new Set(previous)
                        if (next.has(stage.id)) next.delete(stage.id)
                        else next.add(stage.id)
                        return next
                      })
                    }}
                    className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:cursor-default"
                  >
                    {canExpand ? (isExpanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />) : <span className="w-3.5" />}
                    <span className="min-w-0 break-words text-sm font-medium text-foreground">{stage.title}</span>
                    <span className={cn('shrink-0 text-xs', presentation.textClassName)}>{presentation.label}</span>
                    {time && <span className="hidden shrink-0 text-xs text-muted-foreground md:inline">{time}</span>}
                  </button>
                  {stage.source === 'server' && (
                    <PublicationStageActions
                      stage={stage}
                      publication={publication}
                      outcome={outcome}
                      pending={publicationActionPending}
                      onPublish={() => setConfirmPublish(true)}
                      onRetryPublish={() => retryPublish.mutate()}
                      onReconcile={() => reconcile.mutate()}
                      onRecoverDraft={() => recoverDraft.mutate()}
                    />
                  )}
                  {isTerminalRecoveryPoint && onResume && (
                    <Button size="xs" variant="outline" onClick={onResume}>
                      <Send className="size-3" />
                      继续执行
                    </Button>
                  )}
                </div>

                {isExpanded && (
                  <div className="ml-5 mt-2 min-w-0 space-y-2 pb-1 text-sm">
                    {stage.goal && <p className="text-muted-foreground"><span className="text-foreground">目标：</span>{stage.goal}</p>}
                    {stage.latest_update && <p className="break-words text-foreground">{stage.latest_update}</p>}
                    {isTerminalRecoveryPoint && status === 'failed' && <FailureSummary failure={failure} />}
                    {isTerminalRecoveryPoint && status === 'cancelled' && stage.state !== 'cancelled' && (
                      <p className="text-xs text-muted-foreground">执行已停止，可从已有上下文继续。</p>
                    )}
                    {stage.id === lastWorkStageId && <CompletionSummary workflow={workflow} outcome={outcome} />}
                    {stage.kind === 'publication' && publication?.status === 'needs_selection' && (
                      <CandidatePicker publication={publication} pending={selectArticle.isPending} onSelect={(articleId) => selectArticle.mutate(articleId)} />
                    )}
                    {stage.source === 'server' && publication?.last_error && (
                      <p className="flex items-start gap-2 text-xs text-destructive"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" />{publication.last_error}</p>
                    )}
                    {time && <p className="text-xs text-muted-foreground md:hidden">{time}</p>}
                  </div>
                )}
              </div>
            </li>
          )
        })}
      </ol>
      {publicationActionFailed && (
        <p className="mx-4 mt-3 flex items-center gap-2 text-xs text-destructive" role="alert">
          <AlertTriangle className="size-3.5" />公众号操作失败，请稍后重试。
        </p>
      )}

      <Dialog open={confirmPublish} onOpenChange={setConfirmPublish}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认正式发布</DialogTitle>
            <DialogDescription>将当前公众号草稿提交为正式文章。提交后系统会继续检测微信的处理结果。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmPublish(false)}>取消</Button>
            <Button loading={publish.isPending} onClick={() => publish.mutate()}>确认发布</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function RailHeading({ onOpenLogs }: { onOpenLogs: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3 px-4">
      <div className="flex items-center gap-2">
        <ListTree className="size-4 text-muted-foreground" />
        <h2 id="task-execution-heading" className="text-sm font-semibold text-foreground">执行进展</h2>
      </div>
      <Button size="xs" variant="ghost" onClick={onOpenLogs}>
        <ScrollText className="size-3.5" />
        查看完整日志
      </Button>
    </div>
  )
}

function PublicationStageActions({
  stage,
  publication,
  outcome,
  pending,
  onPublish,
  onRetryPublish,
  onReconcile,
  onRecoverDraft,
}: {
  stage: TaskLifecycleStage
  publication?: WechatPublication
  outcome?: TaskOutcome
  pending: boolean
  onPublish: () => void
  onRetryPublish: () => void
  onReconcile: () => void
  onRecoverDraft: () => void
}) {
  const backendLink = (
    <Button
      size="xs"
      variant="outline"
      nativeButton={false}
      render={<a href="https://mp.weixin.qq.com/" target="_blank" rel="noreferrer" aria-label="打开公众号后台" />}
    >
      <ExternalLink className="size-3" />
      公众号后台
    </Button>
  )

  if (stage.kind === 'draft') {
    if (stage.state === 'complete' || stage.state === 'failed') return backendLink
    if (stage.state !== 'blocked') return null
    const uncertain = stage.latest_update?.includes('待确认') || outcome?.publication.action === 'check_wechat'
    if (uncertain) {
      return (
        <div className="flex shrink-0 flex-wrap gap-1.5">
          {backendLink}
          <Button size="xs" variant="outline" loading={pending} onClick={onReconcile}><RefreshCw className="size-3" />检测状态</Button>
        </div>
      )
    }
    return (
      <Button size="xs" variant="outline" loading={pending} onClick={onRecoverDraft}>
        <RefreshCw className="size-3" />重试创建草稿
      </Button>
    )
  }

  if (stage.kind !== 'publication') return null
  if (stage.state === 'complete') {
    if (!publication?.article_url) return backendLink
    return (
      <Button size="xs" variant="outline" nativeButton={false} render={<a href={publication.article_url} target="_blank" rel="noreferrer" />}>
        <ExternalLink className="size-3" />打开文章
      </Button>
    )
  }
  if (stage.state === 'failed') return backendLink
  if (publication?.status === 'needs_selection') return null

  const submitted = hasSubmissionEvidence(publication)
  if (stage.state === 'pending' && publication?.status === 'drafted' && !submitted) {
    return <Button size="xs" loading={pending} onClick={onPublish}><Send className="size-3" />正式发布</Button>
  }
  if (stage.state === 'blocked' && publication?.status === 'awaiting_manual_publish' && publication.draft_media_id) {
    return backendLink
  }
  if (stage.state === 'blocked' && publication?.status === 'unsupported' && publication.draft_media_id) {
    if (submitted) {
      return (
        <Button size="xs" variant="outline" loading={pending} onClick={onReconcile}>
          <RefreshCw className="size-3" />检测状态
        </Button>
      )
    }
    return (
      <Button size="xs" variant="outline" loading={pending} onClick={onRetryPublish}>
        <RefreshCw className="size-3" />重试发布
      </Button>
    )
  }
  return null
}

function CandidatePicker({ publication, pending, onSelect }: { publication: WechatPublication; pending: boolean; onSelect: (articleId: string) => void }) {
  const candidates = publication.candidates ?? []
  if (candidates.length === 0) return <p className="text-xs text-muted-foreground">暂未找到可关联文章，系统会继续检测。</p>
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {candidates.map((candidate) => (
        <button
          key={candidate.article_id}
          type="button"
          disabled={pending}
          onClick={() => onSelect(candidate.article_id)}
          className="min-w-0 rounded-md border border-border px-3 py-2 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:opacity-50"
        >
          <span className="block truncate text-sm font-medium text-foreground">{candidate.title || candidate.article_id}</span>
          {candidate.digest && <span className="mt-0.5 block line-clamp-2 text-xs text-muted-foreground">{candidate.digest}</span>}
        </button>
      ))}
    </div>
  )
}

function FailureSummary({ failure }: { failure: ReturnType<typeof taskFailurePresentation> }) {
  if (!failure) {
    return <p className="text-xs text-muted-foreground">服务端没有返回失败详情，可继续执行并补充说明。</p>
  }
  return (
    <div className="space-y-1 text-xs">
      <p className="font-medium text-destructive">{failure.title}</p>
      <p className="text-muted-foreground">{failure.message}</p>
      {failure.recovery && <p className="text-muted-foreground">{failure.recovery}</p>}
    </div>
  )
}

function CompletionSummary({ workflow, outcome }: { workflow?: WorkflowStatus | string | null; outcome?: TaskOutcome }) {
  const review = useMemo(() => parseWorkflowStatus(workflow)?.review, [workflow])
  if (!review && !outcome) return null
  return (
    <div className="mt-3 border-t border-border pt-3" aria-label="内容验收结果">
      <div className="flex flex-wrap items-center gap-2">
        <FileCheck2 className="size-3.5 text-muted-foreground" />
        <span className="text-xs font-medium text-foreground">内容验收</span>
        {review && <Badge variant="outline" className="text-[10px]">{readinessValueLabel(review.readiness) || '已评估'} · {review.overall_score}</Badge>}
        {outcome && <Badge variant="outline" className="text-[10px]">{outcome.review.status === 'passed' ? '审核通过' : outcome.review.status === 'warning' ? '有警告' : '审核不可用'}</Badge>}
      </div>
      {review?.next_actions?.length ? <p className="mt-2 text-xs text-muted-foreground">下一步：{review.next_actions.slice(0, 2).join('；')}</p> : null}
      {outcome?.warnings.length ? <p className="mt-1 text-xs text-amber-700 dark:text-amber-400">{outcome.warnings.map((warning) => warning.message).join('；')}</p> : null}
    </div>
  )
}

export default TaskExecutionRail
