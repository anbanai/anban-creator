import { useState, useEffect, useRef, useCallback } from 'react'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertTriangle, ArrowLeft, Download, Trash2, RefreshCw, Target, Loader2, ShieldCheck, Send, Ban } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import type { InputAttachment, TaskFile } from '@/types'
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
import { TaskContextSummary } from '@/components/tasks/TaskContextSummary'
import { TaskDetailsSheet, type TaskDetailsTab } from '@/components/tasks/TaskDetailsSheet'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { taskStatusLabel, contentTypeLabel, statusBadgeVariant, progressStageLabel } from '@/lib/labels'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { taskFailureMessage } from '@/lib/studio-ux'

const RESUME_FILE_MAX_BYTES = 25 * 1024 * 1024
const RESUME_ATTACHMENT_POLICY = {
  allowedTypes: GENERAL_AGENT_ATTACHMENT_POLICY.allowedTypes,
  maxCount: GENERAL_AGENT_ATTACHMENT_POLICY.maxCount,
  maxBytes: {
    image: RESUME_FILE_MAX_BYTES,
    audio: RESUME_FILE_MAX_BYTES,
    video: RESUME_FILE_MAX_BYTES,
    document: RESUME_FILE_MAX_BYTES,
    text: RESUME_FILE_MAX_BYTES,
  },
} as const
function ResumeTaskDialog({
  taskId,
  onClose,
  onSuccess,
}: {
  taskId: string
  onClose: () => void
  onSuccess: () => void
}) {
  const [prompt, setPrompt] = useState('')
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: RESUME_ATTACHMENT_POLICY,
  })
  const resumeMutation = useMutation({
    mutationFn: (request: { prompt: string; input_attachments: InputAttachment[] }) => (
      api.tasks.resume(taskId, request)
    ),
    onSuccess: () => {
      toast.success('已提交，任务将结合已有上下文继续执行')
      onSuccess()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '继续执行失败，请稍后再试'))
    },
  })
  const hasInput = Boolean(prompt.trim() || attachmentController.attachments.length > 0)

  return (
    <Dialog
      open
      disablePointerDismissal={resumeMutation.isPending}
      onOpenChange={(open, eventDetails) => {
        if (open) return
        if (resumeMutation.isPending) {
          eventDetails.cancel()
          return
        }
        onClose()
      }}
    >
      <DialogContent className="sm:max-w-2xl" closeButtonDisabled={resumeMutation.isPending}>
        <DialogHeader>
          <DialogTitle>继续执行此任务</DialogTitle>
          <DialogDescription>
            提供补充指令和文件后，任务会结合已有上下文继续执行。
          </DialogDescription>
        </DialogHeader>
        <AgentPromptInput
          value={{ prompt, attachments: attachmentController.attachments }}
          onChange={(value) => setPrompt(value.prompt)}
          onSubmit={async (value) => {
            await resumeMutation.mutateAsync({
              prompt: value.prompt,
              input_attachments: attachmentController.toInputAttachments(),
            })
          }}
          attachmentController={attachmentController}
          attachmentPolicy={RESUME_ATTACHMENT_POLICY}
          placeholder="说明希望 AI 接着做什么，并可添加补充资料或修改意见..."
          ariaLabel="继续任务要求"
          submitLabel="提交并继续"
          submitting={resumeMutation.isPending}
          submitDisabled={!hasInput}
          acceptedTypesLabel="图片、音频、视频、文档、文本；最多 5 个，单个不超过 25MB"
          autoFocus
        />
        <DialogFooter>
          <Button type="button" variant="outline" disabled={resumeMutation.isPending} onClick={onClose}>取消</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const MAX_SSE_LOGS = 500
const TERMINAL_SSE_EVENTS = new Set(['done', 'completed', 'failed', 'cancelled'])

function appendLog(prev: string[], entry: string): string[] {
  const next = [...prev, entry]
  return next.length > MAX_SSE_LOGS ? next.slice(-MAX_SSE_LOGS) : next
}

function splitLogLines(log: string): string[] {
  return log
    .split(/\r?\n/)
    .map((line) => line.trimEnd())
    .filter(Boolean)
}

function appendPollingReplay(prev: string[], replay: string): string[] {
  const currentLines = prev.flatMap(splitLogLines).slice(-MAX_SSE_LOGS)
  const replayLines = splitLogLines(replay).slice(-MAX_SSE_LOGS)
  if (replayLines.length === 0) return prev

  let sharedPrefix = 0
  while (
    sharedPrefix < currentLines.length &&
    sharedPrefix < replayLines.length &&
    currentLines[sharedPrefix] === replayLines[sharedPrefix]
  ) {
    sharedPrefix += 1
  }
  if (sharedPrefix > 0) {
    const newLines = replayLines.slice(sharedPrefix)
    if (newLines.length === 0) return prev
    return [...prev, ...newLines].slice(-MAX_SSE_LOGS)
  }

  const maxOverlap = Math.min(currentLines.length, replayLines.length)
  for (let overlap = maxOverlap; overlap > 0; overlap -= 1) {
    const currentStart = currentLines.length - overlap
    if (replayLines.slice(0, overlap).every((line, index) => line === currentLines[currentStart + index])) {
      const newLines = replayLines.slice(overlap)
      if (newLines.length === 0) return prev
      return [...prev, ...newLines].slice(-MAX_SSE_LOGS)
    }
  }

  return [...prev, ...replayLines].slice(-MAX_SSE_LOGS)
}

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
  const [sseTaskId, setSseTaskId] = useState<string | null>(null)
  const [liveProgress, setLiveProgress] = useState<{
    percent: number
    stage: string | null
    title: string | null
    description: string | null
  } | null>(null)
  const [showCancelDialog, setShowCancelDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showProjectDialog, setShowProjectDialog] = useState(false)
  const [showResumeDialog, setShowResumeDialog] = useState(false)
  const [showCloneDialog, setShowCloneDialog] = useState(false)
  const [showTaskDetails, setShowTaskDetails] = useState(false)
  const [taskDetailsTab, setTaskDetailsTab] = useState<TaskDetailsTab>('overview')
  const [autoScrollLogs, setAutoScrollLogs] = useState(true)
  const abortRef = useRef<AbortController | null>(null)
  const activeSseTaskRef = useRef<string | null>(null)
  const persistedLogsRef = useRef<string[]>([])
  const logContainerRef = useRef<HTMLDivElement | null>(null)
  const { submit } = useSubmitLock()
  const tokenRef = useRef(token)
  tokenRef.current = token

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
    enabled: !!id && !!task && ['completed', 'failed', 'cancelled'].includes(task.status),
  })

  const publishedFiles = files?.filter((file) => file.state !== 'collected') ?? []
  const collectedFiles = files?.filter((file) => file.state === 'collected') ?? []

  // Resolve project info for the task
  const { data: projectDetail } = useQuery({
    queryKey: ['project', task?.project_id],
    queryFn: () => api.projects.get(task!.project_id),
    enabled: !!task?.project_id,
  })
  const project = projectDetail?.project

  const MAX_PERSISTED_LOGS = 500
  const persistedLogs = (task?.progress_log
    ? splitLogLines(task.progress_log)
    : [])
    .slice(-MAX_PERSISTED_LOGS)
  // Lifecycle changes seed from this snapshot; polling updates must not duplicate live entries.
  persistedLogsRef.current = persistedLogs
  const isCurrentSseTask = Boolean(task?.id && sseTaskId === task.id)
  const displayLogs = isCurrentSseTask && sseLogs.length > 0 ? sseLogs : persistedLogs
  const currentSseError = isCurrentSseTask && task?.status === 'running' ? sseError : null
  const currentLiveProgress = isCurrentSseTask && task?.status === 'running' ? liveProgress : null
  // progressValue takes the MAX of live SSE and persisted task.progress to
  // guarantee a monotonic bar. task.progress is the server-side high-water
  // mark (UpdateProgressColumn has a monotonic guard); latest_progress.percent
  // is last-writer-wins and can be lower under out-of-order stage emissions,
  // so it must NOT be the sole source — Math.max keeps the bar from regressing.
  const progressValue = Math.max(0, Math.min(100, Math.max(
    currentLiveProgress?.percent ?? 0,
    task?.progress ?? 0,
  )))
  // Prefer live SSE > server-persisted latest_progress > generic fallback.
  // Never fall back to arbitrary progress_log lines — that column mixes
  // structured JSON with "Using tool: ..." noise and would leak into the card.
  const fallbackTitle = task?.status === 'pending' ? '任务等待执行中...' : '任务执行中...'
  const persistedProgress = task?.latest_progress
  const progressTitle = currentLiveProgress?.title ?? persistedProgress?.title ?? fallbackTitle
  const progressDescription = currentLiveProgress?.description ?? persistedProgress?.description ?? null
  const progressStage = currentLiveProgress?.stage ?? persistedProgress?.stage ?? null
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

	const handleSSEEvent = useCallback((
		taskId: string,
		event: SSEEvent,
    reconcileProgressReplay = false,
	): boolean => {
		if (activeSseTaskRef.current !== taskId) return false

    const parsed = typeof event.data === 'string'
      ? (() => { try { return JSON.parse(event.data) } catch { return event.data } })()
      : event.data

    switch (event.event) {
      case 'progress': {
        if (typeof parsed === 'string') {
          setSseLogs((prev) => reconcileProgressReplay
            ? appendPollingReplay(prev, parsed)
            : appendLog(prev, parsed))
          return true
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
      case 'timeout':
        break
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
        queryClient.invalidateQueries({ queryKey: ['task', taskId] })
        queryClient.invalidateQueries({ queryKey: ['task-files', taskId] })
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
    return false
  }, [queryClient])

  const connectSSE = useCallback(async (taskId: string) => {
    let retries = 0
    let consecutiveTimeouts = 0
    while (activeSseTaskRef.current === taskId) {
      const currentToken = tokenRef.current
      if (!currentToken) return
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller

      try {
        let sawTimeout = false
        let reconcileNextStringProgress = true
        for await (const event of streamTaskProgress(taskId, currentToken, controller.signal)) {
          if (controller.signal.aborted || activeSseTaskRef.current !== taskId) return
          const handledStringProgress = handleSSEEvent(
            taskId,
            event,
            reconcileNextStringProgress,
          )
          if (handledStringProgress) reconcileNextStringProgress = false
          if (TERMINAL_SSE_EVENTS.has(event.event)) return
          if (event.event === 'timeout') {
            sawTimeout = true
          } else {
            consecutiveTimeouts = 0
          }
        }
        if (sawTimeout) {
          retries = 0
          consecutiveTimeouts += 1
          if (consecutiveTimeouts > 1) {
            await new Promise((resolve) => setTimeout(
              resolve,
              Math.min(2000 * (consecutiveTimeouts - 1), 6000),
            ))
            if (controller.signal.aborted || activeSseTaskRef.current !== taskId) return
          }
          continue
        }
        throw new Error('SSE connection closed unexpectedly')
      } catch (err) {
        if (controller.signal.aborted || activeSseTaskRef.current !== taskId) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (retries >= 3) {
          setSseError('连接断开，正在刷新任务状态...')
          queryClient.invalidateQueries({ queryKey: ['task', taskId] })
          return
        }
        retries += 1
        setSseLogs((prev) => appendLog(prev, `连接断开，正在重试 (${retries}/3)...`))
        await new Promise((resolve) => setTimeout(resolve, 2000 * retries))
        if (controller.signal.aborted || activeSseTaskRef.current !== taskId) return
      }
    }
  }, [handleSSEEvent, queryClient])

  useEffect(() => {
    const taskId = task?.id ?? null
    abortRef.current?.abort()
    abortRef.current = null
    activeSseTaskRef.current = task?.status === 'running' ? taskId : null
    setSseTaskId(taskId)
    setSseLogs(task?.status === 'running' ? persistedLogsRef.current : [])
    setSseError(null)
    setLiveProgress(null)

    if (taskId && task?.status === 'running') void connectSSE(taskId)

    return () => {
      if (activeSseTaskRef.current === taskId) activeSseTaskRef.current = null
      abortRef.current?.abort()
      abortRef.current = null
    }
  }, [connectSSE, task?.id, task?.status])

  useEffect(() => {
    setTaskDetailsTab('overview')
  }, [task?.id])

  useEffect(() => {
    setShowResumeDialog(false)
    setShowCloneDialog(false)
  }, [id, task?.id])

  function openTaskDetails(tab: TaskDetailsTab) {
    setTaskDetailsTab(tab)
    setShowTaskDetails(true)
  }

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
  const canClone = task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled'
  const failureMessage = taskFailureMessage(task)
  const currentTask = task
  const snapshot = task.project_snapshot
  const projectDialogPlatform = project?.platform || snapshot?.platform || task.type
  const projectDialogInstructions = project?.instructions || project?.positioning || snapshot?.instructions || '—'
  const projectDialogEcommerceDefaults = project?.ecommerce_defaults || snapshot?.ecommerce_defaults
  const showPendingResultDestination = (task.status === 'pending' || task.status === 'running')
    && publishedFiles.length === 0
    && collectedFiles.length === 0

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
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <Breadcrumb className="mb-2">
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink render={<Link to="/tasks" />}>任务</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator className="hidden sm:list-item" />
              <BreadcrumbItem className="hidden sm:inline-flex">
                <BreadcrumbPage>{task.title || task.prompt || contentTypeLabel[task.type] + ' 任务'}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="min-w-0 max-w-4xl text-xl font-bold leading-tight text-foreground">
              {task.title || task.prompt || contentTypeLabel[task.type] + ' 任务'}
            </h1>
            <Badge variant="outline">
                {renderPlatformIcon(task.type)}
                {contentTypeLabel[task.type] || task.type}
              </Badge>
            <Badge variant={statusBadgeVariant(task.status)}>{taskStatusLabel[task.status] || task.status}</Badge>
            {project && (
              <button
                type="button"
                onClick={() => setShowProjectDialog(true)}
                className="flex items-center gap-1.5 rounded-md bg-card px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
              >
                {project.avatar_url ? (
                  <span className="h-4 w-4 overflow-hidden rounded-full bg-secondary">
                    <SignedImage
                      src={project.avatar_url}
                      alt=""
                      className="h-full w-full object-cover"
                      fallbackClassName="h-full w-full"
                      fallbackIcon={<span className="text-[8px] font-medium text-secondary-foreground">{project.name.charAt(0)}</span>}
                    />
                  </span>
                ) : (
                  <span className="flex h-4 w-4 items-center justify-center rounded-full bg-secondary text-[8px] font-medium">
                    {project.name.charAt(0)}
                  </span>
                )}
                {project.name}
              </button>
            )}
          </div>
        </div>
        <div className="flex w-full flex-wrap items-center gap-2 lg:w-auto lg:justify-end">
          {task.status === 'completed' ? (
            <label className="flex h-7 shrink-0 cursor-pointer items-center gap-2 rounded-md border border-border bg-background px-2.5 text-sm font-medium">
              <Checkbox
                checked={task.published}
                disabled={togglePublished.isPending}
                onCheckedChange={(published) => {
                  void submit(async () => togglePublished.mutateAsync({ published })).catch(() => {})
                }}
              />
              <span>已发布</span>
            </label>
          ) : null}
          {canCancel && (
            <Button
              variant="destructive"
              size="sm"
              loading={cancelMutation.isPending}
              onClick={() => setShowCancelDialog(true)}
            >
              <Ban className="h-4 w-4" />
              取消任务
            </Button>
          )}
          {canClone && task.status !== 'failed' && (
            <Button variant={task.status === 'completed' && !task.published ? 'outline' : 'default'} size="sm" onClick={() => setShowResumeDialog(true)}>
              <Send className="h-4 w-4" />
              继续执行
            </Button>
          )}
          {canClone ? (
            <Button variant="outline" size="sm" onClick={() => setShowCloneDialog(true)}>
              <RefreshCw className="h-4 w-4" />
              克隆任务
            </Button>
          ) : null}
          {canClone ? (
            <Button
              variant="outline"
              size="sm"
              className="text-destructive hover:bg-destructive/5 hover:text-destructive"
              onClick={() => setShowDeleteDialog(true)}
            >
              <Trash2 className="h-4 w-4" />
              删除任务
            </Button>
          ) : null}
        </div>
      </div>

      {task.status !== 'completed' && (
        <section className={`rounded-lg border p-4 ${task.status === 'failed' ? 'border-destructive/35 bg-destructive/5' : 'border-border bg-card'}`}>
          {task.status === 'failed' ? (
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="flex min-w-0 items-start gap-3">
                <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
                <div className="min-w-0">
                  <h2 className="text-sm font-semibold text-foreground">{failureMessage ? '执行失败' : '任务未完成'}</h2>
                  <p className="mt-1 break-words text-sm text-muted-foreground">
                    {failureMessage || '服务端没有返回失败详情，可继续执行并补充说明。'}
                  </p>
                </div>
              </div>
              <Button size="sm" className="shrink-0" onClick={() => setShowResumeDialog(true)}>
                <Send className="h-4 w-4" />
                补充信息并继续
              </Button>
            </div>
          ) : task.status === 'cancelled' ? (
            <div className="flex items-start gap-3">
              <Ban className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
              <div>
                <h2 className="text-sm font-semibold text-foreground">执行已停止</h2>
                <p className="mt-1 text-sm text-muted-foreground">可继续此任务，已有上下文和文件会被保留。</p>
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  {isRunning && <Loader2 className="h-4 w-4 shrink-0 animate-spin text-primary" />}
                  <h2 className="truncate text-sm font-semibold text-foreground">{progressTitle}</h2>
                </div>
                <span className="shrink-0 text-sm font-semibold tabular-nums text-primary">{progressValue}%</span>
              </div>
              <Progress value={progressValue} className="w-full" />
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                {progressStage && <span>{progressStageLabel[progressStage] ?? progressStage}</span>}
                {progressDescription && <span>{progressDescription}</span>}
              </div>
            </div>
          )}
        </section>
      )}

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
                onClick={() => {
                  void submit(async () => approvePublish.mutateAsync()).catch(() => {})
                }}
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
              <p className="mt-0.5 text-xs text-muted-foreground">该文章未发布，可修改后继续执行或克隆任务。</p>
            </div>
          </CardContent>
        </Card>
      )}

      {task.status === 'completed' && (
        <WorkflowReviewSummary workflow={task.workflow_status} />
      )}

      <TaskDetailsSheet
        open={showTaskDetails}
        onOpenChange={setShowTaskDetails}
        selectedTab={taskDetailsTab}
        onTabChange={setTaskDetailsTab}
        task={task}
        project={project}
        files={publishedFiles}
        logs={displayLogs}
        sseError={currentSseError}
        autoScrollLogs={autoScrollLogs}
        onToggleAutoScroll={() => setAutoScrollLogs((previous) => !previous)}
        onCopyLogs={() => {
          navigator.clipboard.writeText(displayLogs.join('\n'))
          toast.success('已复制执行日志')
        }}
        onReconnectLogs={() => {
          if (task.status !== 'running') return
          activeSseTaskRef.current = task.id
          setSseTaskId(task.id)
          setSseError(null)
          void connectSSE(task.id)
        }}
        logContainerRef={logContainerRef}
      />

      {showPendingResultDestination && (
        <section aria-labelledby="task-result-heading" className="border-y border-border py-4">
          <h2 id="task-result-heading" className="px-4 text-sm font-semibold text-foreground">任务结果</h2>
          <Empty className="min-h-24 rounded-none border-0 p-4">
            <EmptyHeader>
              <EmptyTitle>结果生成后将在这里显示</EmptyTitle>
            </EmptyHeader>
          </Empty>
        </section>
      )}

      {/* Files (top priority - most useful content) */}
      {publishedFiles.length > 0 && (
        <Card>
          <div className="border-b border-border px-4 py-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-foreground">生成文件 ({publishedFiles.length})</h2>
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
              <EcommerceFilesGallery
                files={publishedFiles}
                taskId={task.id}
              />
            ) : (
              <>
                {/* Image files in compact grid */}
                {(() => {
                  const imageFiles = publishedFiles.filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
                  if (imageFiles.length === 0) return null
                  return (
                    <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
                      <FilePreviewGallery
                        files={imageFiles}
                        taskId={task.id}
                        taskType={task.type}
                        inlineItemClassName="shrink-0 snap-start"
                      />
                    </div>
                  )
                })()}
                {/* Non-image files share the same full-width preview rows. */}
                {(() => {
                  const nonImageFiles = publishedFiles.filter((f: TaskFile) => !f.mime_type?.startsWith('image/'))
                  if (nonImageFiles.length === 0) return null
                  return (
                    <div className="space-y-2">
                      <FilePreviewGallery
                        files={nonImageFiles}
                        taskId={task.id}
                        taskType={task.type}
                      />
                    </div>
                  )
                })()}
              </>
            )}
          </div>
        </Card>
      )}

      {collectedFiles.length > 0 && (
        <Card>
          <div className="flex items-start gap-3 border-b border-border px-4 py-3">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0">
              <h2 className="text-sm font-semibold text-foreground">失败执行文件 ({collectedFiles.length})</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">诊断与阶段性产物，不包含在成功交付 ZIP 中。</p>
            </div>
          </div>
          <div className="flex flex-col gap-2 p-4">
            <FilePreviewGallery
              files={collectedFiles}
              taskId={task.id}
              taskType={task.type}
            />
          </div>
        </Card>
      )}

      {task.type === 'seednote' && task.published && (
        <SeednoteAnalyticsPanel taskId={task.id} />
      )}

      <TaskContextSummary
        task={task}
        project={project}
        files={publishedFiles}
        logs={displayLogs}
        progressDescription={progressDescription}
        sseError={currentSseError}
        onOpenTab={openTaskDetails}
      />

      {showResumeDialog ? (
        <ResumeTaskDialog
          taskId={currentTask.id}
          onClose={() => setShowResumeDialog(false)}
          onSuccess={() => {
            setShowResumeDialog(false)
            queryClient.invalidateQueries({ queryKey: ['task', id] })
            queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
          }}
        />
      ) : null}

      <TaskFormDialog
        open={showCloneDialog}
        mode="clone"
        sourceTask={currentTask}
        onOpenChange={setShowCloneDialog}
        onCreated={(nextTask) => navigate(`/tasks/${nextTask.id}`)}
      />

      {project && (
        <Dialog open={showProjectDialog} onOpenChange={setShowProjectDialog}>
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>项目信息</DialogTitle>
              <DialogDescription>
                当前任务关联项目的实时配置，用于对照本次任务继承的项目参数。
              </DialogDescription>
            </DialogHeader>
            <div className="max-h-[70vh] overflow-y-auto pr-1">
              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
                <div className="min-w-0">
                  <p className="text-xs text-muted-foreground">项目名称</p>
                  <p className="mt-1 text-base font-semibold text-foreground">{project.name}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">平台</p>
                  <p className="mt-1 text-sm text-foreground">{contentTypeLabel[projectDialogPlatform] || projectDialogPlatform}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">图片比例</p>
                  <p className="mt-1 text-sm text-foreground">{project.image_ratio || snapshot?.image_ratio || '—'}</p>
                </div>
              </div>
              <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-2">
                <div>
                  <p className="text-xs text-muted-foreground">主页</p>
                  <p className="mt-1 break-all text-sm text-foreground">{project.profile_url || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">关键词</p>
                  <p className="mt-1 text-sm text-foreground">{project.keywords || snapshot?.keywords || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">定位 / 说明</p>
                  <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{projectDialogInstructions}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">视觉风格</p>
                  <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{project.visual_style || snapshot?.visual_style || '—'}</p>
                </div>
              </div>
              {projectDialogPlatform === 'article' && (
                <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-3">
                  <div>
                    <p className="text-xs text-muted-foreground">署名</p>
                    <p className="mt-1 text-sm text-foreground">{project.author || snapshot?.author || '—'}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">写作风格</p>
                    <p className="mt-1 text-sm text-foreground">{project.writer || snapshot?.writer || '默认'}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">排版</p>
                    <p className="mt-1 text-sm text-foreground">{project.theme || snapshot?.theme || '默认'}</p>
                  </div>
                </div>
              )}
              {projectDialogPlatform === 'ecommerce' && projectDialogEcommerceDefaults && (
                <div className="mt-4 grid gap-3 border-t border-border pt-4 sm:grid-cols-3">
                  <div>
                    <p className="text-xs text-muted-foreground">默认目标平台</p>
                    <p className="mt-1 text-sm text-foreground">{projectDialogEcommerceDefaults.target_platform || '—'}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">默认模块</p>
                    <p className="mt-1 text-sm text-foreground">
                      {projectDialogEcommerceDefaults.default_selected_modules
                        ? Object.entries(projectDialogEcommerceDefaults.default_selected_modules).map(([k, v]) => `${k} x${v}`).join('、')
                        : '—'}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">图片模型</p>
                    <p className="mt-1 text-sm text-foreground">{projectDialogEcommerceDefaults.image_model_key || '—'}</p>
                  </div>
                  <div className="sm:col-span-3">
                    <p className="text-xs text-muted-foreground">品牌 brief</p>
                    <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{projectDialogEcommerceDefaults.brand_brief || '—'}</p>
                  </div>
                </div>
              )}
            </div>
          </DialogContent>
        </Dialog>
      )}

      {/* Cancel confirmation */}
      <AlertDialog open={showCancelDialog} onOpenChange={setShowCancelDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定取消此任务？</AlertDialogTitle>
            <AlertDialogDescription>
              取消后会立即停止尚未完成的执行步骤。用户主动取消不撤销已确认的任务固定价；已成功交付的增值操作也会保留对应费用。此操作不可撤销。
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
