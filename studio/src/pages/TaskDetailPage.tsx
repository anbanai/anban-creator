import { useState, useEffect, useRef } from 'react'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Streamdown } from 'streamdown'
import { ArrowLeft, Download, Eye, Trash2, Copy, RefreshCw, Target, Loader2, ShieldCheck, Send, Ban } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { formatUSD } from '@/lib/utils'
import type { TaskFile } from '@/types'
import { streamTaskProgress, type SSEEvent } from '@/lib/sse'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { FilePreviewGallery } from '@/components/FilePreview'
import { EcommerceFilesGallery } from '@/components/tasks/EcommerceFilesGallery'
import { SignedImage } from '@/components/ui/SignedImage'
import { WorkflowReviewSummary } from '@/components/TaskWorkflowPanel'
import SeednoteAnalyticsPanel from '@/components/tasks/SeednoteAnalyticsPanel'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { taskStatusLabel, contentTypeLabel, formatFullDateTimeCN, statusBadgeVariant, progressStageLabel } from '@/lib/labels'
import { renderPlatformIcon } from '@/lib/PlatformIcon'

export default function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  // Guard against /tasks/undefined from stale or malformed navigation.
  useEffect(() => {
    if (!id || id === 'undefined') {
      navigate('/tasks', { replace: true })
    }
  }, [id, navigate])

  const queryClient = useQueryClient()
  const { token } = useAuth()

  const [sseLogs, setSseLogs] = useState<string[]>([])
  const [sseError, setSseError] = useState<string | null>(null)
  const [liveProgress, setLiveProgress] = useState<{
    percent: number
    stage: string | null
    title: string | null
    description: string | null
  } | null>(null)
  const [showCancelDialog, setShowCancelDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [autoScrollLogs, setAutoScrollLogs] = useState(true)
  const abortRef = useRef<AbortController | null>(null)
  const logContainerRef = useRef<HTMLDivElement | null>(null)
  const { submit } = useSubmitLock()
  const tokenRef = useRef(token)
  tokenRef.current = token

  const MAX_SSE_LOGS = 500
  function appendLog(prev: string[], entry: string): string[] {
    const next = [...prev, entry]
    return next.length > MAX_SSE_LOGS ? next.slice(-MAX_SSE_LOGS) : next
  }

  const { data: task, isLoading, isError, refetch } = useQuery({
    queryKey: ['task', id],
    queryFn: () => api.tasks.get(id!),
    refetchInterval: (query) => {
      const t = query.state.data
      if (t && (t.status === 'running' || t.status === 'pending')) return 5000
      return false
    },
    enabled: !!id,
  })

  const { data: files } = useQuery({
    queryKey: ['task-files', id],
    queryFn: () => api.tasks.files(id!),
    enabled: !!id && task?.status === 'completed',
  })

  // Resolve project info for the task
  const { data: projectDetail } = useQuery({
    queryKey: ['project', task?.project_id],
    queryFn: () => api.projects.get(task!.project_id),
    enabled: !!task?.project_id,
  })
  const project = projectDetail?.project

  const MAX_PERSISTED_LOGS = 500
  const persistedLogs = (task?.progress_log
    ?.split('\n')
    .map((line) => line.trimEnd())
    .filter(Boolean) ?? [])
    .slice(-MAX_PERSISTED_LOGS)
  const displayLogs = sseLogs.length > 0 ? sseLogs : persistedLogs
  // 行尾两空格 + \n 是 Markdown 的硬换行语法（<br>），避免单 \n 被 marked 当作 soft break 塌缩成空格。
  const logMarkdown = displayLogs.join('  \n')
  const showLogs = displayLogs.length > 0 || Boolean(sseError) || task?.status === 'running'
  // progressValue takes the MAX of live SSE and persisted task.progress to
  // guarantee a monotonic bar. task.progress is the server-side high-water
  // mark (UpdateProgressColumn has a monotonic guard); latest_progress.percent
  // is last-writer-wins and can be lower under out-of-order stage emissions,
  // so it must NOT be the sole source — Math.max keeps the bar from regressing.
  const progressValue = Math.max(0, Math.min(100, Math.max(
    liveProgress?.percent ?? 0,
    task?.progress ?? 0,
  )))
  // Prefer live SSE > server-persisted latest_progress > generic fallback.
  // Never fall back to arbitrary progress_log lines — that column mixes
  // structured JSON with "Using tool: ..." noise and would leak into the card.
  const fallbackTitle = task?.status === 'pending' ? '任务等待执行中...' : '任务执行中...'
  const persistedProgress = task?.latest_progress
  const progressTitle = liveProgress?.title ?? persistedProgress?.title ?? fallbackTitle
  const progressDescription = liveProgress?.description ?? persistedProgress?.description ?? null
  const progressStage = liveProgress?.stage ?? persistedProgress?.stage ?? null
  const isRunning = task?.status === 'running'

  const cancelMutation = useMutation({
    mutationFn: () => api.tasks.cancel(id!),
    onSuccess: () => {
      toast.success('任务已取消')
      abortRef.current?.abort()
      abortRef.current = null
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
      setShowCancelDialog(false)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '取消任务失败，请稍后重试'))
    },
  })

  const togglePublished = useMutation({
    mutationFn: ({ published }: { published: boolean }) =>
      api.tasks.markPublished(id!, published),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
      if (id) {
        queryClient.invalidateQueries({ queryKey: queryKeys.tasks.seednoteAnalytics(id) })
      }
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '切换发布状态失败，请稍后重试'))
    },
  })

  // Publish-approval gate (Batch 4A): resume a held publish or close the gate.
  const approvePublish = useMutation({
    mutationFn: () => api.tasks.publishApprove(id!),
    onSuccess: () => {
      toast.success('已放行，正在发布到公众号草稿箱')
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '放行失败，请稍后重试'))
    },
  })

  const rejectPublish = useMutation({
    mutationFn: () => api.tasks.publishReject(id!),
    onSuccess: () => {
      toast.success('已驳回发布审核')
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '驳回失败，请稍后重试'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => api.tasks.delete(id!),
    onSuccess: () => {
      toast.success('任务已删除')
      abortRef.current?.abort()
      abortRef.current = null
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
      setShowDeleteDialog(false)
      navigate('/tasks')
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '删除失败，请稍后重试'))
    },
  })

  const connectSSE = async (retries = 0) => {
    if (!id) return
    const currentToken = tokenRef.current
    if (!currentToken) return

    // Abort any existing connection
    if (abortRef.current) {
      abortRef.current.abort()
    }
    const controller = new AbortController()
    abortRef.current = controller

    try {
      for await (const event of streamTaskProgress(id, currentToken, controller.signal)) {
        handleSSEEvent(event)
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (retries < 3 && !controller.signal.aborted) {
        setSseLogs((prev) => appendLog(prev, `连接断开，正在重试 (${retries + 1}/3)...`))
        await new Promise((r) => setTimeout(r, 2000 * (retries + 1)))
        if (controller.signal.aborted) return
        return connectSSE(retries + 1)
      }
      setSseError('连接断开，正在刷新任务状态...')
      queryClient.invalidateQueries({ queryKey: ['task', id] })
    }
  }

  function handleSSEEvent(event: SSEEvent) {
    const parsed = typeof event.data === 'string'
      ? (() => { try { return JSON.parse(event.data) } catch { return event.data } })()
      : event.data

    switch (event.event) {
      case 'progress': {
        if (typeof parsed === 'string') {
          setSseLogs((prev) => appendLog(prev, parsed))
        } else {
          const data = parsed as {
            stage?: string
            title?: string
            description?: string
            percent?: number
          }
          const pct = typeof data.percent === 'number' ? data.percent : null
          const hasPositivePct = pct != null && pct > 0
          // 任何结构化字段（stage/title/description/percent）到位都更新 liveProgress，
          // 即使 percent === 0（未知 stage 时 server 默认 0）。否则卡片会回退到通用文案，
          // 与同一时刻写入 SSE 日志面板的结构化数据不一致。
          if (pct != null || data.title || data.stage || data.description) {
            setLiveProgress({
              percent: pct ?? 0,
              stage: data.stage ?? null,
              title: data.title ?? null,
              description: data.description ?? null,
            })
          }
          // Display priority: title (with description) → stage → bare percent.
          // Structured events always carry title via the MCP tool contract.
          const label = data.title
            ? `${data.title}${data.description ? ' · ' + data.description : ''}`
            : (data.stage || '')
          if (label) {
            const prefix = hasPositivePct ? `[${pct as number}%] ` : ''
            setSseLogs((prev) => appendLog(prev, `${prefix}${label}`))
          }
        }
        break
      }
      case 'output': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { text?: string }).text || ''
        if (data) {
          setSseLogs((prev) => appendLog(prev, data))
        }
        break
      }
      case 'error': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { error?: string }).error || 'Unknown error'
        setSseLogs((prev) => appendLog(prev, `错误：${data}`))
        break
      }
      case 'done':
      case 'completed':
      case 'failed':
      case 'cancelled': {
        queryClient.invalidateQueries({ queryKey: ['task', id] })
        queryClient.invalidateQueries({ queryKey: ['task-files', id] })
        const statusText = event.event === 'completed' ? '任务完成'
          : event.event === 'failed' ? '任务失败'
            : event.event === 'cancelled' ? '任务取消' : '任务完成'
        setSseLogs((prev) => appendLog(prev, `--- ${statusText} ---`))
        break
      }
      default: {
        const text = typeof parsed === 'string' ? parsed : JSON.stringify(parsed)
        if (text && text !== '{}' && text.length > 0) {
          setSseLogs((prev) => appendLog(prev, text))
        }
      }
    }
  }

  // Connect SSE only when task status transitions to "running"
  useEffect(() => {
    if (task?.status === 'running') {
      setSseLogs(persistedLogs)
      setSseError(null)
      setLiveProgress(null)
      connectSSE()
    }
    return () => {
      if (abortRef.current) {
        abortRef.current.abort()
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.status])

  useEffect(() => {
    if (!autoScrollLogs || !logContainerRef.current) return
    logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
  }, [displayLogs, autoScrollLogs])

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="space-y-2">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-6 w-64" />
          <div className="flex gap-2 mt-2">
            <Skeleton className="h-5 w-16" />
            <Skeleton className="h-5 w-16" />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-5 space-y-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-6 w-20" />
            </div>
          ))}
        </div>
        <Skeleton className="h-40 w-full rounded-lg" />
      </div>
    )
  }

  if (isError) {
    return (
      <div className="space-y-4">
        <button
          onClick={() => navigate('/tasks')}
          className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          返回任务列表
        </button>
        <QueryErrorState onRetry={() => refetch()} />
      </div>
    )
  }

  if (!task) {
    return (
      <div className="text-center py-16">
        <p className="text-muted-foreground">任务未找到</p>
        <Button variant="ghost" className="mt-3" onClick={() => navigate('/tasks')}>
          返回任务列表
        </Button>
      </div>
    )
  }

  const canCancel = task.status === 'pending' || task.status === 'running'
  const canRetry = task.status === 'failed' || task.status === 'cancelled'
  const currentTask = task

  // Retry re-runs this task as a fresh billed task. The server clones the full
  // configuration (three-dimensional style, author/writer, ecommerce package,
  // image model, watermark, goal mode…) so nothing is lost — unlike the previous
  // client-side create() which only forwarded type/prompt/project/ratio.
  async function handleRetry() {
    await submit(async () => {
      try {
        const nextTask = await api.tasks.retry(currentTask.id)
        toast.success('已重新创建任务，全部设置已保留')
        queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
        navigate(`/tasks/${nextTask.id}`)
      } catch (err: unknown) {
        const status = (err as { response?: { status?: number; data?: { code?: number } } })?.response?.status
        const code = (err as { response?: { data?: { code?: number } } })?.response?.data?.code
        if (status === 402 || code === 40200) {
          toast.error('积分不足，无法重试')
        } else {
          toast.error(getApiErrorMessage(err, '重试失败，请稍后再试'))
        }
      }
    })
  }

  return (
    <div className="space-y-6">
      {/* Screen reader live region for status changes */}
      <div className="sr-only" aria-live="polite" aria-atomic="true">
        {task?.status === 'completed' && '任务已完成'}
        {task?.status === 'failed' && '任务失败'}
        {task?.status === 'cancelled' && '任务已取消'}
      </div>

      {/* Goal mode banner */}
      {task.goal_mode && (
        <div className="rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-700 dark:bg-amber-950/40">
          <div className="flex items-start gap-2">
            <Target className="mt-0.5 h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-amber-900 dark:text-amber-100">
                强目标模式
              </p>
              <p className="mt-1 text-sm text-amber-800 dark:text-amber-200">
                <span className="font-medium">目标条件：</span>
                {task.goal || '(未设置)'}
              </p>
              <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">
                AI 会自动检查产出是否符合目标条件，未达成会继续修订直到符合（或达到最大尝试次数）。
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Header */}
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <Breadcrumb className="mb-2">
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink render={<Link to="/tasks" />}>任务</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{task.title || task.prompt || contentTypeLabel[task.type] + ' 任务'}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-xl font-bold text-foreground">{task.title || task.prompt || contentTypeLabel[task.type] + ' 任务'}</h1>
            <Badge variant="outline">
                {renderPlatformIcon(task.type)}
                {contentTypeLabel[task.type] || task.type}
              </Badge>
            <Badge variant={statusBadgeVariant(task.status)}>{taskStatusLabel[task.status] || task.status}</Badge>
            {project && (
              <Link
                to={`/projects`}
                className="flex items-center gap-1.5 rounded-md bg-card px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              >
                {project.avatar_url ? (
                  <img src={project.avatar_url} alt="" className="h-4 w-4 rounded-full object-cover" />
                ) : (
                  <span className="flex h-4 w-4 items-center justify-center rounded-full bg-secondary text-[8px] font-medium">
                    {project.name.charAt(0)}
                  </span>
                )}
                {project.name}
              </Link>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {task.status === 'completed' && (
            <Button
              variant={task.published ? 'outline' : 'default'}
              size="sm"
              loading={togglePublished.isPending}
              onClick={() => { void submit(async () => togglePublished.mutateAsync({ published: !task.published })).catch(() => {}) }}
            >
              <Eye className="h-4 w-4" />
              {task.published ? '已发布' : '标记已发布'}
            </Button>
          )}
          {canCancel && (
            <Button
              variant="destructive"
              size="sm"
              loading={cancelMutation.isPending}
              onClick={() => setShowCancelDialog(true)}
            >
              取消任务
            </Button>
          )}
          {!canCancel && (
            <Button
              variant="ghost"
              size="sm"
              loading={deleteMutation.isPending}
              onClick={() => setShowDeleteDialog(true)}
              className="text-red-400 hover:text-red-300 hover:bg-red-900/20"
            >
              <Trash2 className="h-4 w-4" />
              删除
            </Button>
          )}
          {canRetry && (
            <Button variant="outline" size="sm" onClick={() => void handleRetry()}>
              <RefreshCw className="h-4 w-4" />
              重新执行
            </Button>
          )}
        </div>
      </div>

      {/* Publish-approval gate (Batch 4A): the project requires human review
          before publishing, so a completed article draft is held here until the
          user explicitly approves (→ WeChat draft box) or rejects it. */}
      {task.publish_approval_state === 'pending' && (
        <Card className="border-amber-500/40 bg-amber-500/10">
          <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-3">
              <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-amber-500" />
              <div>
                <p className="text-sm font-medium text-foreground">发布待审核</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  文章草稿已完成，已暂停自动发布。确认无误后放行，将发布到公众号草稿箱（非直接群发）。
                </p>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Button
                size="sm"
                loading={approvePublish.isPending}
                onClick={() => { void submit(async () => approvePublish.mutateAsync()).catch(() => {}) }}
              >
                <Send className="h-4 w-4" />
                放行发布
              </Button>
              <Button
                variant="outline"
                size="sm"
                loading={rejectPublish.isPending}
                onClick={() => { void submit(async () => rejectPublish.mutateAsync()).catch(() => {}) }}
              >
                <Ban className="h-4 w-4" />
                驳回
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
      {task.publish_approval_state === 'approved' && (
        <Card className="border-emerald-500/40 bg-emerald-500/10">
          <CardContent className="flex items-start gap-3">
            <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-emerald-500" />
            <div>
              <p className="text-sm font-medium text-foreground">已通过发布审核</p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {task.published
                  ? '草稿已放行，已发布到公众号草稿箱。'
                  : '草稿已放行，正在发布到公众号草稿箱…'}
              </p>
            </div>
          </CardContent>
        </Card>
      )}
      {task.publish_approval_state === 'rejected' && (
        <Card className="bg-muted/30">
          <CardContent className="flex items-start gap-3">
            <Ban className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
            <div>
              <p className="text-sm font-medium text-foreground">已驳回发布</p>
              <p className="mt-0.5 text-xs text-muted-foreground">该文章未发布，可修改后重新执行任务。</p>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Details (stats) */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardContent>
            <p className="text-xs text-muted-foreground">创建时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.created_at)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent>
            <p className="text-xs text-muted-foreground">开始时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.started_at)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent>
            <p className="text-xs text-muted-foreground">完成时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.completed_at)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent>
            <p className="text-xs text-muted-foreground">来源</p>
            <p className="mt-1 text-sm text-foreground">{task.plan_id ? '计划任务' : '手动创建'}</p>
          </CardContent>
        </Card>
      </div>

      {task.type === 'seednote' && task.published && (
        <SeednoteAnalyticsPanel taskId={task.id} />
      )}

      {task.project_snapshot && task.project_snapshot.platform && (
        <Card>
          <CardContent className="space-y-3">
            <div>
              <p className="text-xs text-muted-foreground">项目快照</p>
              <p className="mt-1 text-sm font-medium text-foreground">
                {task.project_snapshot.project_name || project?.name || '—'}
              </p>
            </div>
            <div className="grid gap-3 lg:grid-cols-[140px_minmax(0,1fr)]">
              <div>
                <p className="text-xs text-muted-foreground">参考图</p>
                {task.project_snapshot.reference_image_url ? (
                  <a
                    href={task.project_snapshot.reference_image_url}
                    target="_blank"
                    rel="noreferrer"
                    className="mt-1 block h-24 w-32 overflow-hidden rounded-md border border-border bg-muted"
                  >
                    <SignedImage
                      src={task.project_snapshot.reference_image_url}
                      alt="参考图"
                      className="h-full w-full object-cover"
                      fallbackClassName="h-full w-full"
                    />
                  </a>
                ) : (
                  <div className="mt-1 flex h-24 w-32 items-center justify-center rounded-md border border-dashed border-border bg-muted/40 text-xs text-muted-foreground">
                    未设置
                  </div>
                )}
              </div>
              <div className="min-w-0">
                <p className="text-xs text-muted-foreground">视觉风格</p>
                <p className="mt-1 line-clamp-4 text-sm text-foreground">{task.project_snapshot.visual_style || '—'}</p>
              </div>
            </div>
            <div className="grid gap-3 border-t border-border pt-3 sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <p className="text-xs text-muted-foreground">图片比例</p>
                <p className="mt-1 text-sm text-foreground">{task.project_snapshot.image_ratio || task.image_ratio || '—'}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">图片模型</p>
                <p className="mt-1 text-sm text-foreground">{task.image_model_key || task.project_snapshot.ecommerce_defaults?.image_model_key || '—'}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">扣除积分</p>
                <p className="mt-1 text-sm text-foreground">{typeof task.credits_charged === 'number' ? task.credits_charged.toLocaleString() : '—'}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">执行成本</p>
                <p className="mt-1 text-sm text-foreground">{task.total_cost_usd && task.total_cost_usd > 0 ? formatUSD(task.total_cost_usd) : '—'}</p>
              </div>
            </div>
            {task.type === 'article' && (
              <div className="grid gap-3 border-t border-border pt-3 sm:grid-cols-3">
                <div>
                  <p className="text-xs text-muted-foreground">署名</p>
                  <p className="mt-1 text-sm text-foreground">{task.project_snapshot.author || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">写作风格</p>
                  <p className="mt-1 text-sm text-foreground">{task.project_snapshot.writer || '默认'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">排版</p>
                  <p className="mt-1 text-sm text-foreground">{task.project_snapshot.theme || '默认'}</p>
                </div>
              </div>
            )}
            {task.type === 'ecommerce' && task.project_snapshot.ecommerce_defaults && (
              <div className="grid gap-3 border-t border-border pt-3 sm:grid-cols-2 lg:grid-cols-3">
                <div>
                  <p className="text-xs text-muted-foreground">目标平台</p>
                  <p className="mt-1 text-sm text-foreground">{task.project_snapshot.ecommerce_defaults.target_platform || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">默认模块</p>
                  <p className="mt-1 text-sm text-foreground">
                    {task.project_snapshot.ecommerce_defaults.default_selected_modules
                      ? Object.entries(task.project_snapshot.ecommerce_defaults.default_selected_modules).map(([k, v]) => `${k} x${v}`).join('、')
                      : '—'}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">品牌 brief</p>
                  <p className="mt-1 line-clamp-3 text-sm text-foreground">{task.project_snapshot.ecommerce_defaults.brand_brief || '—'}</p>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {task.status !== 'completed' && (
        <Card>
          <CardContent>
            {task.status === 'failed' ? (
            <div className="space-y-3">
              <p className="text-sm text-red-400">任务失败</p>
              {task.error && (
                <p className="mt-2 bg-red-900/20 border border-red-900/30 rounded-lg px-3 py-2 text-sm text-red-300">
                  {task.error}
                </p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={() => void handleRetry()}>
                  <RefreshCw className="h-4 w-4" />
                  重新执行
                </Button>
                <Button variant="ghost" size="sm" onClick={() => navigate('/tasks')}>
                  返回任务列表
                </Button>
                <Button variant="ghost" size="sm" onClick={() => navigate('/projects')}>
                  检查项目配置
                </Button>
              </div>
            </div>
            ) : task.status === 'cancelled' ? (
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-sm text-muted-foreground">任务已取消</p>
              <Button variant="outline" size="sm" onClick={() => void handleRetry()}>
                <RefreshCw className="h-4 w-4" />
                再试一次
              </Button>
            </div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  {progressStage ? (
                    <span className="text-xs text-muted-foreground">
                      阶段：{progressStageLabel[progressStage] ?? progressStage}
                    </span>
                  ) : (
                    <span className="text-xs text-muted-foreground/60">执行中</span>
                  )}
                  <span className="shrink-0 text-sm font-semibold text-primary tabular-nums">
                    {progressValue}%
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  {isRunning && <Loader2 className="h-4 w-4 shrink-0 animate-spin text-primary" />}
                  <span className="min-w-0 truncate text-base font-semibold text-foreground">
                    {progressTitle}
                  </span>
                </div>
                <Progress value={progressValue} className="w-full" />
                {progressDescription && (
                  <p className="text-xs text-muted-foreground">{progressDescription}</p>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {task.status === 'completed' && (
        <WorkflowReviewSummary workflow={task.workflow_status} />
      )}

      {/* Files (top priority - most useful content) */}
      {files && files.length > 0 && (
        <Card>
          <div className="border-b border-border px-4 py-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-foreground">生成文件 ({files.length})</h2>
            <Button
              size="sm"
              onClick={async () => {
                try {
                  const blob = await api.tasks.downloadZipBlob(task.id)
                  const url = URL.createObjectURL(blob)
                  const a = document.createElement('a')
                  a.href = url
                  a.download = `task_${task.id}_files.zip`
                  a.click()
                  URL.revokeObjectURL(url)
                } catch (err) {
                  toast.error('下载 ZIP 失败，请稍后重试')
                }
              }}
            >
              <Download className="h-4 w-4" />
              下载全部 (ZIP)
            </Button>
          </div>
          <div className="p-4 space-y-4">
            {task.type === 'ecommerce' ? (
              <EcommerceFilesGallery files={files} taskId={task.id} />
            ) : (
              <>
                {/* Image files in compact grid */}
                {(() => {
                  const imageFiles = files.filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
                  if (imageFiles.length === 0) return null
                  return (
                    <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
                      <FilePreviewGallery files={imageFiles} taskId={task.id} inlineItemClassName="shrink-0 snap-start" />
                    </div>
                  )
                })()}
                {/* HTML files */}
                {(() => {
                  const htmlFiles = files.filter((f: TaskFile) => f.mime_type === 'text/html')
                  if (htmlFiles.length === 0) return null
                  return (
                    <div className="space-y-3">
                      <FilePreviewGallery files={htmlFiles} taskId={task.id} />
                    </div>
                  )
                })()}
                {/* Other files */}
                {(() => {
                  const otherFiles = files.filter((f: TaskFile) => !f.mime_type?.startsWith('image/') && f.mime_type !== 'text/html')
                  if (otherFiles.length === 0) return null
                  return (
                    <div className="space-y-2">
                      <FilePreviewGallery files={otherFiles} taskId={task.id} />
                    </div>
                  )
                })()}
              </>
            )}
          </div>
        </Card>
      )}

      {/* Live Output / SSE Logs */}
      {showLogs && (
        <Card>
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold text-foreground">执行日志</h2>
            <div className="flex items-center gap-2">
              <Button
                variant="ghost"
                size="xs"
                onClick={() => setAutoScrollLogs((prev) => !prev)}
              >
                {autoScrollLogs ? '跟随输出' : '暂停跟随'}
              </Button>
              <Button
                variant="ghost"
                size="xs"
                onClick={() => {
                  navigator.clipboard.writeText(displayLogs.join('\n'))
                  toast.success('已复制执行日志')
                }}
                disabled={displayLogs.length === 0}
              >
                <Copy className="h-3.5 w-3.5" />
                复制
              </Button>
            </div>
          </div>
          <div ref={logContainerRef} className="max-h-96 overflow-y-auto bg-background/50 px-4 py-3">
            {sseError && (
              <div className="mb-2 flex items-center gap-2">
                <p className="text-xs text-amber-400">{sseError}</p>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 gap-1 px-2 text-xs text-amber-400 hover:text-amber-300"
                  onClick={() => {
                    setSseError(null)
                    connectSSE(0)
                  }}
                >
                  <RefreshCw className="h-3 w-3" />
                  重新连接
                </Button>
              </div>
            )}
            {displayLogs.length === 0 ? (
              <p className="text-xs text-muted-foreground">等待输出中...</p>
            ) : (
              <Streamdown mode="streaming" className="prose prose-sm max-w-none dark:prose-invert">
                {logMarkdown}
              </Streamdown>
            )}
          </div>
        </Card>
      )}

      {/* Result output for completed tasks */}
      {task.status === 'completed' && task.result?.output && (
        <Card>
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold text-foreground">执行结果</h2>
          </div>
          <div className="max-h-64 overflow-y-auto bg-background/50 px-4 py-3 prose prose-sm max-w-none dark:prose-invert">
            <Streamdown mode="static">
              {task.result.output}
            </Streamdown>
          </div>
        </Card>
      )}

      {/* Cancel confirmation */}
      <AlertDialog open={showCancelDialog} onOpenChange={setShowCancelDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定取消此任务？</AlertDialogTitle>
            <AlertDialogDescription>
              {task.status === 'pending' ? (
                <>此任务尚未开始执行，取消后将<strong className="text-foreground">全额退还已扣积分</strong>，不会产生任何费用。{' '}</>
              ) : (
                <>
                  任务正在执行中，取消后将立即停止未完成的步骤。
                  {task.total_cost_usd && task.total_cost_usd > 0 ? (
                    <>已完成步骤（AI 写作、图片生成等）已消耗约 <strong className="text-foreground">{formatUSD(task.total_cost_usd)}</strong>，<strong className="text-foreground">不予退还</strong>；其余将退还。{' '}</>
                  ) : (
                    <>已完成步骤（如 AI 写作、图片生成）的费用<strong className="text-foreground">不予退还</strong>，其余将退还。{' '}</>
                  )}
                </>
              )}
              此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>再想想</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={cancelMutation.isPending} onClick={() => { void submit(async () => cancelMutation.mutateAsync()).catch(() => {}) }}>
              {cancelMutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
              确定取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete confirmation */}
      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定删除此任务？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后任务及所有关联文件将被永久移除，此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>再想想</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { void submit(async () => deleteMutation.mutateAsync()).catch(() => {}) }}>
              {deleteMutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
              确定删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
