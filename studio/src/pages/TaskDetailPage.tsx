import { useState, useEffect, useRef, useCallback } from 'react'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowLeft, BarChart3, Ban, FolderOpen, Loader2, MessageSquare, RefreshCw, Trash2 } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink } from '@/components/ui/breadcrumb'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { taskContentAnalyticsHref } from '@/lib/content-analytics'
import { queryKeys } from '@/lib/query-keys'
import type { InputAttachment, Task, TaskLifecycle } from '@/types'
import { streamTaskProgress, type SSEEvent } from '@/lib/sse'
import { useAuth } from '@/contexts/AuthContext'
import { Button, buttonVariants } from '@/components/common/button'
import { StatusPill } from '@/components/workspace'
import SeednoteAnalyticsPanel from '@/components/tasks/SeednoteAnalyticsPanel'
import WechatAnalyticsPanel from '@/components/tasks/WechatAnalyticsPanel'
import ChannelsAnalyticsPanel from '@/components/tasks/ChannelsAnalyticsPanel'
import { CompletionSummary, TaskExecutionRail } from '@/components/tasks/TaskExecutionRail'
import { TaskDeliverablesPanel } from '@/components/tasks/TaskDeliverablesPanel'
import { TaskContextSummary } from '@/components/tasks/TaskContextSummary'
import { TaskDetailsSheet, type TaskDetailsTab } from '@/components/tasks/TaskDetailsSheet'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { ImageCapabilityDisplay } from '@/components/ImageCapabilityDisplay'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { taskStatusLabel, platformDisplayName } from '@/lib/labels'
import { shouldApplyLifecycleRevision, shouldStreamTaskLifecycle } from '@/lib/task-lifecycle'
import { normalizeProjectProfile } from '@/lib/project-profile'
import { supportsProfileLab } from '@/lib/project-capabilities'
import TaskFeedbackCard from '@/components/tasks/TaskFeedbackCard'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { AgentIconStack, agentDisplayName } from '@/components/agent-prompt/AgentIconStack'

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
  shouldHandleResult,
}: {
  taskId: string
  onClose: () => void
  onSuccess: () => void
  shouldHandleResult: (taskId: string) => boolean
}) {
  const queryClient = useQueryClient()
  const [prompt, setPrompt] = useState('')
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: RESUME_ATTACHMENT_POLICY,
  })
  const resumeMutation = useMutation({
    mutationFn: ({ initiatingTaskId, request }: {
      initiatingTaskId: string
      request: { prompt: string; input_attachments: InputAttachment[] }
    }) => (
      api.tasks.resume(initiatingTaskId, request)
    ),
    onSuccess: (_data, { initiatingTaskId }) => {
      if (!shouldHandleResult(initiatingTaskId)) return
      toast.success('已提交，作品将结合已有上下文继续执行')
      onSuccess()
    },
    onError: (err, { initiatingTaskId }) => {
      if (!shouldHandleResult(initiatingTaskId)) return
      toast.error(getApiErrorMessage(err, '继续执行失败，请稍后再试'))
    },
    onSettled: (_data, _error, { initiatingTaskId }) => Promise.all([
      queryClient.invalidateQueries({ queryKey: ['task', initiatingTaskId] }),
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all }),
    ]),
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
          <DialogTitle>继续执行此作品</DialogTitle>
          <DialogDescription>
            提供补充指令和文件后，作品会结合已有上下文继续执行。
          </DialogDescription>
        </DialogHeader>
        <AgentPromptInput
          value={{ prompt, attachments: attachmentController.attachments }}
          onChange={(value) => setPrompt(value.prompt)}
          onSubmit={async (value) => {
            await resumeMutation.mutateAsync({
              initiatingTaskId: taskId,
              request: {
                prompt: value.prompt,
                input_attachments: attachmentController.toInputAttachments(),
              },
            })
          }}
          attachmentController={attachmentController}
          attachmentPolicy={RESUME_ATTACHMENT_POLICY}
          placeholder="说明希望 AI 接着做什么，并可添加补充资料或修改意见..."
          ariaLabel="继续作品要求"
          submitLabel="提交并继续"
          submitting={resumeMutation.isPending}
          submitDisabled={!hasInput}
          acceptedTypesLabel="图片、音频、视频、文档、文本；最多 5 个，单个不超过 25MB"
          autoFocus
        />
        <DialogFooter>
          <Button type="button" variant="outline" className="min-h-11" disabled={resumeMutation.isPending} onClick={onClose}>取消</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const MAX_SSE_LOGS = 500
const TERMINAL_SSE_EVENTS = new Set(['done', 'completed', 'failed', 'cancelled'])

const profileDimensionLabels: Record<string, string> = {
  identity: '定位',
  style: '风格',
  audience: '受众',
  platforms: '平台',
  preferences: '偏好与红线',
  memory: '经验记忆',
}

const profileStatusLabels: Record<string, string> = {
  not_started: '尚未建立',
  queued: '等待分析',
  running: '分析中',
  ready: '已就绪',
  failed: '分析失败',
}

function profileDimensionSummary(content: Record<string, unknown>) {
  const preferredKeys = ['summary', 'description', 'positioning', 'tone', 'audience', 'content']
  for (const key of preferredKeys) {
    const value = content[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  const values = Object.values(content)
    .filter((value): value is string | number | boolean => ['string', 'number', 'boolean'].includes(typeof value))
    .map(String)
    .filter(Boolean)
  return values.slice(0, 2).join(' · ') || '尚未补充'
}

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
  const [liveLifecycle, setLiveLifecycle] = useState<TaskLifecycle | null>(null)
  const [showCancelDialog, setShowCancelDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showProjectDialog, setShowProjectDialog] = useState(false)
  const { items: imageCapabilities } = useImageCapabilities(showProjectDialog)
  const [showResumeDialog, setShowResumeDialog] = useState(false)
  const [showCloneDialog, setShowCloneDialog] = useState(false)
  const [cloneSourceTask, setCloneSourceTask] = useState<Task | null>(null)
  const [showTaskDetails, setShowTaskDetails] = useState(false)
  const [showFeedback, setShowFeedback] = useState(false)
  const [manualExecutionProcess, setManualExecutionProcess] = useState<{ taskId: string; open: boolean } | null>(null)
  const [taskDetailsTab, setTaskDetailsTab] = useState<TaskDetailsTab>('overview')
  const [autoScrollLogs, setAutoScrollLogs] = useState(true)
  const abortRef = useRef<AbortController | null>(null)
  const activeSseTaskRef = useRef<string | null>(null)
  const persistedLogsRef = useRef<string[]>([])
  const lifecycleRevisionRef = useRef(0)
  const logContainerRef = useRef<HTMLDivElement | null>(null)
  const cloneSourceIdentityRef = useRef<{ routeId: string; taskId: string } | null>(null)
  const { submit } = useSubmitLock()
  const tokenRef = useRef(token)
  tokenRef.current = token

  useEffect(() => {
    setShowFeedback(false)
  }, [id])

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

  useEffect(() => {
    if (task?.status !== 'completed') setShowFeedback(false)
  }, [task?.status])

  const activeTaskIdentityRef = useRef<{ routeId: string | undefined; taskId: string | undefined }>({
    routeId: id,
    taskId: task?.id,
  })
  activeTaskIdentityRef.current = { routeId: id, taskId: task?.id }
  const isTaskActive = useCallback((taskId: string) => {
    const active = activeTaskIdentityRef.current
    return active.routeId === taskId && active.taskId === taskId
  }, [])

  const { data: files, isLoading: filesLoading, isError: filesError, refetch: refetchFiles } = useQuery({
    queryKey: ['task-files', id],
    queryFn: () => api.tasks.files(id!),
    enabled: !!id && !!task && ['completed', 'failed', 'cancelled'].includes(task.status),
  })

  const deliveredFiles = files?.filter((file) => file.state === 'delivered') ?? []
  const deliverableFiles = deliveredFiles.filter((file) => file.is_deliverable === true)
  const hasDeliverableResult = files?.some((file) => (
    (file.state === 'delivered' || file.state === 'retained') && file.is_deliverable === true
  )) ?? false
  const hasPreservedDelivery = task?.status === 'failed' && deliverableFiles.length > 0
  const hasPendingServerPublication = task?.lifecycle?.stages.some((stage) => (
    stage.source === 'server'
      && stage.kind === 'publication'
      && (stage.state === 'pending' || stage.state === 'blocked')
  )) ?? false
  const defaultExecutionProcessOpen = task?.status !== 'completed'
    || filesLoading
    || (files !== undefined && !hasDeliverableResult)
    || filesError
    || hasPendingServerPublication
  const showExecutionProcess = task && manualExecutionProcess?.taskId === task.id
    ? manualExecutionProcess.open
    : defaultExecutionProcessOpen

  // Resolve project info for the task
  const { data: projectDetail } = useQuery({
    queryKey: ['project', task?.project_id],
    queryFn: () => api.projects.get(task!.project_id),
    enabled: !!task?.project_id,
  })
  const project = projectDetail?.project
  const projectProfileQuery = useQuery({
    queryKey: ['project-profile', project?.id],
    queryFn: () => api.projects.getAccountProfile(project!.id),
    enabled: showProjectDialog && Boolean(project?.id) && Boolean(project && supportsProfileLab(project)),
    staleTime: 30_000,
  })
  const projectProfile = normalizeProjectProfile(projectProfileQuery.data ?? project?.profile)

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
  const persistedLifecycleRevision = task?.lifecycle?.revision ?? 0
  if (persistedLifecycleRevision > lifecycleRevisionRef.current) lifecycleRevisionRef.current = persistedLifecycleRevision
  const displayLifecycle = liveLifecycle && liveLifecycle.revision > persistedLifecycleRevision
    ? liveLifecycle
    : task?.lifecycle
  const executionStages = displayLifecycle?.stages ?? []
  const summaryStage = executionStages.find((stage) => ['active', 'blocked', 'failed', 'cancelled'].includes(stage.state))
    ?? executionStages.find((stage) => stage.state === 'pending')
    ?? [...executionStages].reverse().find((stage) => stage.kind === 'work')
    ?? executionStages[executionStages.length - 1]
  const nextExecutionStep = task?.status === 'completed'
    ? '检查交付结果'
    : task?.status === 'failed'
      ? '查看原因并按需恢复'
      : task?.status === 'cancelled'
        ? '按需重新开始'
        : task?.status === 'pending'
          ? '等待开始'
          : executionStages.find((stage) => stage.state === 'pending')?.title ?? '持续查看进度'
  const executionSummary = task
    ? `阶段：${summaryStage?.title ?? (task.status === 'pending' ? '等待开始' : '执行计划')} · 状态：${taskStatusLabel[task.status] || task.status} · 下一步：${nextExecutionStep}`
    : ''
  const streamLifecycle = task ? shouldStreamTaskLifecycle(task) : false
  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => api.tasks.cancel(taskId),
    onSuccess: (_data, taskId) => {
      if (!isTaskActive(taskId)) return
      toast.success('作品已取消')
      abortRef.current?.abort()
      abortRef.current = null
      setShowCancelDialog(false)
    },
    onError: (err, taskId) => {
      if (!isTaskActive(taskId)) return
      toast.error(getApiErrorMessage(err, '取消作品失败，请稍后重试'))
    },
    onSettled: (_data, _error, taskId) => Promise.all([
      queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all }),
    ]),
  })

  const deleteMutation = useMutation({
    mutationFn: (taskId: string) => api.tasks.delete(taskId),
    onSuccess: (_data, taskId) => {
      if (!isTaskActive(taskId)) return
      toast.success('作品已删除')
      abortRef.current?.abort()
      abortRef.current = null
      setShowDeleteDialog(false)
      navigate('/tasks')
    },
    onError: (err, taskId) => {
      if (!isTaskActive(taskId)) return
      toast.error(getApiErrorMessage(err, '删除失败，请稍后重试'))
    },
    onSettled: (_data, _error, taskId) => Promise.all([
      queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all }),
    ]),
  })

  const handleSSEEvent = useCallback((
    taskId: string,
    event: SSEEvent,
  ): void => {
    if (activeSseTaskRef.current !== taskId) return

    const parsed = typeof event.data === 'string'
      ? (() => { try { return JSON.parse(event.data) } catch { return event.data } })()
      : event.data

    switch (event.event) {
      case 'lifecycle': {
        if (typeof parsed === 'string') break
        const next = parsed as TaskLifecycle
        if (next.version !== 1 || !Array.isArray(next.stages)) break
        if (!shouldApplyLifecycleRevision(lifecycleRevisionRef.current, next.revision)) break
        lifecycleRevisionRef.current = next.revision
        setLiveLifecycle(next)
        break
      }
      case 'log': {
        const data = typeof parsed === 'string' ? parsed : JSON.stringify(parsed)
        if (data) setSseLogs((prev) => appendPollingReplay(prev, data))
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
        const statusText = event.event === 'completed' ? '作品完成'
          : event.event === 'failed' ? '创作失败'
            : event.event === 'cancelled' ? '作品取消' : '作品完成'
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
        for await (const event of streamTaskProgress(taskId, currentToken, controller.signal)) {
          if (controller.signal.aborted || activeSseTaskRef.current !== taskId) return
          handleSSEEvent(taskId, event)
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
          setSseError('连接断开，正在刷新作品状态...')
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
    activeSseTaskRef.current = streamLifecycle ? taskId : null
    setSseTaskId(taskId)
    setSseLogs(task?.status === 'running' ? persistedLogsRef.current : [])
    setSseError(null)
    setLiveLifecycle(null)
    lifecycleRevisionRef.current = task?.lifecycle?.revision ?? 0

    if (taskId && streamLifecycle) void connectSSE(taskId)

    return () => {
      if (activeSseTaskRef.current === taskId) activeSseTaskRef.current = null
      abortRef.current?.abort()
      abortRef.current = null
    }
  }, [connectSSE, streamLifecycle, task?.id])

  useEffect(() => {
    setTaskDetailsTab('overview')
  }, [task?.id])

  useEffect(() => {
    if (showTaskDetails && taskDetailsTab === 'logs') {
      setAutoScrollLogs(true)
    }
  }, [showTaskDetails, taskDetailsTab])

  useEffect(() => {
    setShowCancelDialog(false)
    setShowDeleteDialog(false)
    setShowProjectDialog(false)
    setShowResumeDialog(false)
    setShowCloneDialog(false)
    setShowTaskDetails(false)
    setCloneSourceTask(null)
    cloneSourceIdentityRef.current = null
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
          className="flex min-h-11 items-center gap-1 rounded-md text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
        >
          <ArrowLeft className="h-4 w-4" />
          返回作品列表
        </button>
        <QueryErrorState onRetry={() => refetch()} />
      </div>
    )
  }

  if (!task) {
    return (
      <div className="text-center py-16">
        <p className="text-muted-foreground">作品未找到</p>
        <Button variant="ghost" className="mt-3 min-h-11" onClick={() => navigate('/tasks')}>
          返回作品列表
        </Button>
      </div>
    )
  }

  const canCancel = task.status === 'pending' || task.status === 'running'
  const canClone = task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled'
  const canResume = task.status === 'failed' || task.status === 'cancelled'
  // 内容分析 only covers 公众号/种草笔记 账号，montage 等作品没有对应的数据看板。
  const canViewContentAnalytics = task.status === 'completed' && (task.type === 'wechat-article' || task.type === 'seednote')
  const currentTask = task
  const snapshot = task.project_snapshot
  const projectDialogPlatform = project?.platform || snapshot?.platform || task.type
  const projectDialogInstructions = project?.instructions || project?.positioning || snapshot?.instructions || '—'
  const projectDialogEcommerceDefaults = project?.ecommerce_defaults || snapshot?.ecommerce_defaults
  const taskAgentId = task.agent_id || snapshot?.agent_id || task.channel || snapshot?.channel || task.type
  const taskAgentLabel = agentDisplayName(taskAgentId)

  function openCloneDialog() {
    cloneSourceIdentityRef.current = { routeId: id!, taskId: currentTask.id }
    setCloneSourceTask(currentTask)
    setShowCloneDialog(true)
  }

  function handleCloneOpenChange(open: boolean) {
    setShowCloneDialog(open)
    if (open) return
    cloneSourceIdentityRef.current = null
    setCloneSourceTask(null)
  }

  function shouldHandleCloneResult() {
    const source = cloneSourceIdentityRef.current
    const active = activeTaskIdentityRef.current
    return source !== null
      && source.routeId === active.routeId
      && source.taskId === active.taskId
  }

  return (
    <div className="space-y-6">
      {/* Screen reader live region for status changes */}
      <div className="sr-only" aria-live="polite" aria-atomic="true">
        {task.status === 'completed' && '作品已完成'}
        {task?.status === 'failed' && (hasPreservedDelivery ? '续跑失败，历史交付已保留' : '创作失败')}
        {task?.status === 'cancelled' && '作品已取消'}
      </div>

      {/* Header */}
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <Breadcrumb className="mb-2">
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink render={<Link to="/tasks" />}>作品</BreadcrumbLink>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex shrink-0 items-center gap-2" title={taskAgentLabel}>
              <AgentIconStack agentIds={[taskAgentId]} />
              <span className="sr-only">{taskAgentLabel}</span>
            </div>
            <h1 className="min-w-0 max-w-4xl text-xl font-bold leading-tight text-foreground">
              {(task.title || task.prompt || '').length > 90 ? `${taskAgentLabel} · ${task.status === 'completed' ? '创作结果' : '创作任务'}` : task.title || task.prompt || `${taskAgentLabel}作品`}
            </h1>
            <StatusPill status={task.status} label={taskStatusLabel[task.status] || task.status} />
            {project && (
              <span className="max-w-[min(18rem,42vw)] truncate text-xs text-muted-foreground">
                {project.name}
              </span>
            )}
            {project && (
              <Tooltip>
                <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-md" className="hover:bg-transparent dark:hover:bg-transparent focus-visible:bg-transparent" aria-label={`查看人设详情：${project.name}`} onClick={() => setShowProjectDialog(true)} />}>
                  <FolderOpen className="size-4" aria-hidden="true" />
                </TooltipTrigger>
                <TooltipContent>查看人设详情</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
        <div className="flex w-full flex-wrap items-center gap-2 lg:w-auto lg:justify-end">
          {canViewContentAnalytics && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Link
                    to={taskContentAnalyticsHref(task.project_id, task.id, task.channel || task.type)}
                    aria-label="查看内容分析"
                    className={buttonVariants({ variant: 'ghost', size: 'icon-md', className: 'hover:bg-transparent dark:hover:bg-transparent focus-visible:bg-transparent' })}
                  />
                }
              >
                <BarChart3 className="size-4" />
              </TooltipTrigger>
              <TooltipContent>查看内容分析</TooltipContent>
            </Tooltip>
          )}
          {task.status === 'completed' && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-md"
                    className="hover:bg-transparent dark:hover:bg-transparent focus-visible:bg-transparent"
                    aria-label="人工评价"
                    aria-pressed={showFeedback}
                    onClick={() => setShowFeedback(true)}
                  />
                }
              >
                <MessageSquare className="size-4" />
              </TooltipTrigger>
              <TooltipContent>人工评价</TooltipContent>
            </Tooltip>
          )}
          {canCancel && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-md"
                    className="text-destructive hover:bg-transparent dark:hover:bg-transparent hover:text-destructive focus-visible:bg-transparent"
                    aria-label="取消作品"
                    loading={cancelMutation.isPending}
                    onClick={() => setShowCancelDialog(true)}
                  />
                }
              >
                {!cancelMutation.isPending ? <Ban className="size-4" aria-hidden="true" /> : null}
              </TooltipTrigger>
              <TooltipContent>取消作品</TooltipContent>
            </Tooltip>
          )}
          {canClone ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-md"
                    className="hover:bg-transparent dark:hover:bg-transparent focus-visible:bg-transparent"
                    aria-label={task.type === 'hypit' ? '再次改编' : '克隆作品'}
                    onClick={openCloneDialog}
                  />
                }
              >
                <RefreshCw className="size-4" aria-hidden="true" />
              </TooltipTrigger>
              <TooltipContent>{task.type === 'hypit' ? '再次改编' : '克隆作品'}</TooltipContent>
            </Tooltip>
          ) : null}
          {canClone ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-md"
                    className="text-destructive hover:bg-transparent dark:hover:bg-transparent hover:text-destructive focus-visible:bg-transparent"
                    aria-label="删除作品"
                    onClick={() => setShowDeleteDialog(true)}
                  />
                }
              >
                <Trash2 className="size-4" aria-hidden="true" />
              </TooltipTrigger>
              <TooltipContent>删除作品</TooltipContent>
            </Tooltip>
          ) : null}
        </div>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_18rem] xl:items-start">
        <div className="min-w-0 space-y-6">
          <TaskDeliverablesPanel
            taskId={task.id}
            taskStatus={task.status}
            files={files ?? []}
            taskType={task.type}
            filesLoading={filesLoading}
            filesError={filesError}
            onRetryFiles={() => void refetchFiles()}
          />
          {(task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled') && (
            <CompletionSummary workflow={task.workflow_status} outcome={task.outcome} />
          )}

          <Collapsible
            open={showExecutionProcess}
            onOpenChange={(open) => setManualExecutionProcess({ taskId: task.id, open })}
          >
            <div className="rounded-lg border border-border/70 bg-card">
              <CollapsibleTrigger aria-label="执行过程" className="flex min-h-12 w-full items-center justify-between gap-3 px-4 py-2 text-left text-sm font-semibold text-foreground hover:bg-muted/30 sm:px-5">
                <span className="min-w-0 space-y-0.5">
                  <span className="block">执行过程</span>
                  <span className="block break-words text-xs font-normal leading-relaxed text-muted-foreground">{executionSummary}</span>
                </span>
                <span className="shrink-0 text-xs font-normal text-muted-foreground">{showExecutionProcess ? '收起' : '展开查看'}</span>
              </CollapsibleTrigger>
              <CollapsibleContent className="border-t border-border/70 p-0">
          <TaskExecutionRail
            key={task.id}
            taskId={task.id}
            status={task.status}
            lifecycle={displayLifecycle}
            workflow={task.workflow_status}
            outcome={task.outcome}
            errorMessage={task.error_message}
            onOpenLogs={() => openTaskDetails('logs')}
            onResume={canResume ? () => setShowResumeDialog(true) : undefined}
            showFailureSummary
            files={files}
            taskType={task.type}
            filesLoading={filesLoading}
            filesError={filesError}
            onRetryFiles={() => void refetchFiles()}
            showCompletionSummary={false}
            showFiles={false}
          />
              </CollapsibleContent>
            </div>
          </Collapsible>

          {task.type === 'wechat-article' && task.status === 'completed' && project && (
            <WechatAnalyticsPanel taskId={task.id} />
          )}

          {task.type === 'seednote' && task.status === 'completed' && (
            <SeednoteAnalyticsPanel taskId={task.id} />
          )}

          {task.type === 'montage' && task.status === 'completed' && (
            <ChannelsAnalyticsPanel taskId={task.id} />
          )}
        </div>

        <aside className="min-w-0" aria-label="作品辅助信息">
          <TaskContextSummary
            compact
            layout="sidebar"
            task={task}
            project={project}
            files={deliveredFiles}
            onOpenTab={openTaskDetails}
          />
        </aside>
      </div>

      <TaskDetailsSheet
        open={showTaskDetails}
        onOpenChange={setShowTaskDetails}
        selectedTab={taskDetailsTab}
        onTabChange={setTaskDetailsTab}
        task={task}
        project={project}
        files={deliveredFiles}
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

      {showResumeDialog ? (
        <ResumeTaskDialog
          taskId={currentTask.id}
          onClose={() => setShowResumeDialog(false)}
          onSuccess={() => {
            setShowResumeDialog(false)
          }}
          shouldHandleResult={isTaskActive}
        />
      ) : null}

      {showCloneDialog && canClone && cloneSourceTask ? (
        <TaskFormDialog
          open
          mode="clone"
          sourceTask={cloneSourceTask}
          onOpenChange={handleCloneOpenChange}
          onCreated={(nextTask) => navigate(`/tasks/${nextTask.id}`)}
          shouldHandleResult={shouldHandleCloneResult}
        />
      ) : null}

      {task.status === 'completed' && (
        <Dialog open={showFeedback} onOpenChange={setShowFeedback}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>人工评价</DialogTitle>
              <DialogDescription>请为本次作品产出评分，帮助我们持续改进。</DialogDescription>
            </DialogHeader>
            <TaskFeedbackCard taskId={task.id} enabled={showFeedback} showHeader={false} />
          </DialogContent>
        </Dialog>
      )}

      {project && (
        <Dialog open={showProjectDialog} onOpenChange={setShowProjectDialog}>
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>人设详情</DialogTitle>
              <DialogDescription>
                展示当前作品关联的人设实时配置，用于对照本次继承的参数。
              </DialogDescription>
            </DialogHeader>
            <div className="max-h-[70vh] overflow-y-auto pr-1">
              {snapshot ? (
                <div className="mb-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-950 dark:text-amber-100">
                  当前作品使用的是创建时冻结配置；下方人设信息是实时配置，可能与作品快照不同。
                </div>
              ) : null}
              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
                <div className="min-w-0">
                  <p className="text-xs text-muted-foreground">创作项目身份</p>
                  <ProjectIdentity project={project} className="mt-1" />
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">平台</p>
                  <p className="mt-1 text-sm text-foreground">{platformDisplayName(projectDialogPlatform)}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">
                    {projectDialogPlatform === 'montage' ? '视频比例' : '图片比例'}
                  </p>
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
              {projectDialogPlatform === 'wechat' && (
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
                    <p className="text-xs text-muted-foreground">图像能力</p>
                    <p className="mt-1 text-sm text-foreground"><ImageCapabilityDisplay option={imageCapabilities.find((option) => option.key === projectDialogEcommerceDefaults.image_capability_key)} fallback={projectDialogEcommerceDefaults.image_capability_key ? '已停用能力' : '标准图像'} /></p>
                  </div>
                  <div className="sm:col-span-3">
                    <p className="text-xs text-muted-foreground">品牌 brief</p>
                    <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{projectDialogEcommerceDefaults.brand_brief || '—'}</p>
                  </div>
                </div>
              )}
              {project.profile_setup?.platform_accounts?.length ? (
                <section aria-label="账号信息" className="mt-4 border-t border-border pt-4">
                  <p className="text-xs text-muted-foreground">账号信息</p>
                  <div className="mt-2 grid gap-2 sm:grid-cols-2">
                    {project.profile_setup.platform_accounts.map((account, index) => (
                      <div key={`${account.platform}-${account.profile_url}-${index}`} className="min-w-0 rounded-md border border-border/70 bg-muted/20 px-3 py-2 text-sm">
                        <p className="font-medium text-foreground">{platformDisplayName(account.platform)}</p>
                        <p className="mt-1 break-all text-muted-foreground">{account.account_name || account.profile_url || '待补充'}</p>
                        {account.account_name && account.profile_url ? <p className="mt-1 break-all text-xs text-muted-foreground">{account.profile_url}</p> : null}
                      </div>
                    ))}
                  </div>
                </section>
              ) : null}
              <section aria-label="项目画像" className="mt-4 border-t border-border pt-4">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div>
                    <p className="text-xs text-muted-foreground">画像资料</p>
                    <p className="mt-1 text-sm font-medium text-foreground">
                      {projectProfileQuery.isFetching ? '正在读取画像' : projectProfile ? `${profileStatusLabels[projectProfile.initialization_status] || '未标记'} · v${projectProfile.version}` : '暂无画像资料'}
                    </p>
                  </div>
                  {projectProfile && <span className="rounded-full border border-border px-2 py-0.5 text-xs text-muted-foreground">{projectProfile.status === 'confirmed' ? '已确认' : '草稿'}</span>}
                </div>
                {projectProfileQuery.isError ? (
                  <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
                    <span>画像资料加载失败</span>
                    <Button type="button" variant="outline" size="sm" onClick={() => void projectProfileQuery.refetch()}>重试</Button>
                  </div>
                ) : projectProfile ? (
                  <div className="mt-3 grid gap-2 sm:grid-cols-2">
                    {Object.entries(projectProfile.dimensions).map(([name, dimension]) => (
                      <div key={name} className="min-w-0 rounded-md border border-border/70 bg-muted/20 px-3 py-2">
                        <p className="text-xs font-medium text-muted-foreground">{profileDimensionLabels[name] || name}</p>
                        <p className="mt-1 line-clamp-3 text-sm text-foreground">{profileDimensionSummary(dimension.content)}</p>
                      </div>
                    ))}
                  </div>
                ) : !projectProfileQuery.isFetching ? (
                  <p className="mt-3 text-sm text-muted-foreground">当前人设还没有画像资料。</p>
                ) : null}
                {projectProfile?.last_error ? <p className="mt-2 text-xs text-destructive">{projectProfile.last_error}</p> : null}
              </section>
            </div>
          </DialogContent>
        </Dialog>
      )}

      {/* Cancel confirmation */}
      <AlertDialog open={showCancelDialog} onOpenChange={setShowCancelDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定取消此作品？</AlertDialogTitle>
            <AlertDialogDescription>
              取消后会立即停止尚未完成的执行步骤。用户主动取消不撤销已确认的作品固定价；已成功交付的增值操作也会保留对应费用。此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>再想想</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={cancelMutation.isPending} onClick={() => { void submit(async () => cancelMutation.mutateAsync(task.id)).catch(() => {}) }}>
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
            <AlertDialogTitle>确定删除此作品？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后作品及所有关联文件将被永久移除，此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>再想想</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { void submit(async () => deleteMutation.mutateAsync(task.id)).catch(() => {}) }}>
              {deleteMutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
              确定删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
