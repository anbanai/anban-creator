import { useState, useEffect, useRef, type ChangeEvent } from 'react'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Streamdown } from 'streamdown'
import { ArrowLeft, Download, Eye, Trash2, Copy, RefreshCw, Target, Loader2, ShieldCheck, Send, Ban, Upload, X } from 'lucide-react'
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
import { WorkflowReviewSummary, WorkflowStageProgress } from '@/components/TaskWorkflowPanel'
import SeednoteAnalyticsPanel from '@/components/tasks/SeednoteAnalyticsPanel'
import { VideoProductionPanel } from '@/components/video/VideoProductionPanel'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { taskStatusLabel, contentTypeLabel, formatFullDateTimeCN, statusBadgeVariant, progressStageLabel, transactionTypeLabel } from '@/lib/labels'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { videoCreativeTypeLabel, videoModelDisplayName, videoPurposeLabel } from '@/lib/video-display'
import { formatCreditDescription } from '@/lib/credit-display'

interface ResumeFileInput {
  id: string
  file: File
  label: string
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
  const [showCreditDialog, setShowCreditDialog] = useState(false)
  const [resumePrompt, setResumePrompt] = useState('')
  const [resumeFiles, setResumeFiles] = useState<ResumeFileInput[]>([])
  const [autoScrollLogs, setAutoScrollLogs] = useState(true)
  const abortRef = useRef<AbortController | null>(null)
  const logContainerRef = useRef<HTMLDivElement | null>(null)
  const resumeFileInputRef = useRef<HTMLInputElement | null>(null)
  const { submit, isSubmitting } = useSubmitLock()
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

  const { data: videoProduction } = useQuery({
    queryKey: ['task-video-production', id],
    queryFn: () => api.tasks.videoProduction(id!),
    enabled: !!id && task?.type === 'video',
  })

  // Resolve project info for the task
  const { data: projectDetail } = useQuery({
    queryKey: ['project', task?.project_id],
    queryFn: () => api.projects.get(task!.project_id),
    enabled: !!task?.project_id,
  })
  const project = projectDetail?.project
  const isVideoTaskFile = (file: TaskFile) => file.mime_type?.startsWith('video/') || /\.(mp4|mov|webm|m4v)$/i.test(file.file_name)

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
  const creditTransactions = task?.credit_transactions ?? []
  const creditSummary = task?.credits_summary
  const taskConsumedCredits = creditSummary?.task_consumed ?? task?.credits_charged ?? 0
  const operationConsumedCredits = creditSummary?.operation_consumed ?? 0
  const refundedCredits = creditSummary?.refunded ?? 0
  const netConsumedCredits = creditSummary?.net_consumed ?? task?.credits_charged ?? 0
  const showCreditDetails = Boolean(
    task && (
      typeof task.credits_charged === 'number' ||
      creditSummary ||
      creditTransactions.length > 0
    ),
  )

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

  const resumeMutation = useMutation({
    mutationFn: () => api.tasks.resume(id!, {
      prompt: resumePrompt,
      files: resumeFiles.map((item) => item.file),
      fileLabels: resumeFiles.map((item) => item.label),
    }),
    onSuccess: () => {
      toast.success('已提交，任务将基于原目录继续执行')
      setShowResumeDialog(false)
      setResumePrompt('')
      setResumeFiles([])
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '继续执行失败，请稍后再试'))
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
  const canClone = task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled'
  const currentTask = task
  const snapshot = task.project_snapshot
  const showProjectParameters = Boolean(snapshot?.platform || project)
  const showWorkflowStages = Boolean(task.workflow_status) && task.status !== 'pending' && task.status !== 'running'
  const projectParameterName = snapshot?.project_name || project?.name || '—'
  const projectParameterVisualStyle = snapshot?.visual_style || project?.visual_style || '—'
  const projectParameterImageRatio = snapshot?.image_ratio || project?.image_ratio || task.image_ratio || '—'
  const projectParameterImageModel = task.image_model_key || snapshot?.ecommerce_defaults?.image_model_key || project?.ecommerce_defaults?.image_model_key || '—'
  const projectDialogPlatform = project?.platform || snapshot?.platform || task.type
  const projectDialogInstructions = project?.instructions || project?.positioning || snapshot?.instructions || '—'
  const projectDialogEcommerceDefaults = project?.ecommerce_defaults || snapshot?.ecommerce_defaults
  const videoTargetDuration = task.video_config?.target_duration_seconds || task.video_config?.pricing_breakdown?.output_seconds || task.video_config?.duration
  const videoSegmentCount = task.video_config?.segments?.length || task.video_config?.pricing_breakdown?.segment_count || 0
  const videoSpecSummary = [
    task.video_config?.resolution || '—',
    task.video_config?.ratio || '—',
    videoTargetDuration ? `目标 ${videoTargetDuration}s` : '目标 —',
    videoSegmentCount > 0 ? `${videoSegmentCount} 段` : null,
  ].filter(Boolean).join(' · ')
  const videoInputReferences = task.video_config?.references ?? []
  const showVideoInputParameters = task.type === 'video' && Boolean(task.video_config)
  const videoCreativeType = videoCreativeTypeLabel(task.video_config?.creative_type)
  const videoPurpose = videoPurposeLabel(task.video_config?.purpose)
  const videoSubjectProfile = task.video_config?.subject_profile?.trim() || '—'
  const videoAudience = task.video_config?.audience?.trim() || '—'
  const videoSingleMessage = task.video_config?.single_message?.trim() || '—'
  const renderVideoPreviewDetails = (file: TaskFile) => {
    if (!isVideoTaskFile(file)) return null
    return (
      <div className="space-y-3 rounded-lg border border-border p-3 text-sm">
        <p className="text-sm font-medium text-foreground">创作参数</p>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <p className="text-xs text-muted-foreground">内容类型</p>
            <p className="mt-1 text-foreground">{videoCreativeType}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">商业目标</p>
            <p className="mt-1 text-foreground">{videoPurpose}</p>
          </div>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">人物 / 主体</p>
          <p className="mt-1 whitespace-pre-wrap text-foreground">{videoSubjectProfile}</p>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <p className="text-xs text-muted-foreground">生成任务 ID</p>
            <p className="mt-1 break-all text-foreground">{task.video_generation_id || '—'}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">规格</p>
            <p className="mt-1 text-foreground">{videoSpecSummary}</p>
          </div>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">费用明细</p>
          <p className="mt-1 text-foreground">
            估算 {(task.video_estimated_credits || task.video_config?.estimated_credits || 0).toLocaleString()} ·
            已消耗 {(task.video_credits_charged || task.credits_charged || 0).toLocaleString()}
          </p>
          {task.video_config?.pricing_breakdown && (
            <p className="mt-1 text-xs text-muted-foreground">
              {videoModelDisplayName(task.video_config.pricing_breakdown.model_key)} · {task.video_config.pricing_breakdown.resolution} · 输出 {task.video_config.pricing_breakdown.output_seconds}s
              {task.video_config.pricing_breakdown.input_video && typeof task.video_config.pricing_breakdown.input_seconds === 'number'
                ? ` · 输入视频 ${task.video_config.pricing_breakdown.input_seconds}s`
                : ''}
            </p>
          )}
        </div>
        <div>
          <p className="text-xs text-muted-foreground">参考素材</p>
          {task.video_config?.references && task.video_config.references.length > 0 ? (
            <div className="mt-1 divide-y divide-border rounded-md border border-border">
              {task.video_config.references.map((ref, index) => (
                <div key={`${ref.type}-${ref.url || ref.text}-${index}`} className="min-w-0 px-2 py-1.5 text-xs">
                  <p className="truncate text-foreground">{ref.reference_role || ref.type} · {ref.file_name || ref.text || ref.url || '—'}</p>
                  {ref.input_duration_seconds && (
                    <p className="mt-0.5 text-muted-foreground">输入时长 {ref.input_duration_seconds}s</p>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">未使用参考素材</p>
          )}
        </div>
      </div>
    )
  }

  // Clone this task as a fresh billed task. The server clones the full
  // configuration (three-dimensional style, author/writer, ecommerce package,
  // image model, watermark, goal mode…) so nothing is lost — unlike the previous
  // client-side create() which only forwarded type/prompt/project/ratio.
  async function handleClone() {
    await submit(async () => {
      try {
        const nextTask = await api.tasks.clone(currentTask.id)
        toast.success('已克隆为新任务，配置已保留')
        queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
        navigate(`/tasks/${nextTask.id}`)
      } catch (err: unknown) {
        const status = (err as { response?: { status?: number; data?: { code?: number } } })?.response?.status
        const code = (err as { response?: { data?: { code?: number } } })?.response?.data?.code
        if (status === 402 || code === 40200) {
          toast.error('积分不足，无法克隆任务')
        } else {
          toast.error(getApiErrorMessage(err, '克隆任务失败，请稍后再试'))
        }
      }
    })
  }

  function handleVideoRetake(action: string) {
    toast.info(`已选择返修决策：${action}`)
    void handleClone()
  }

  function handleResumeFilesChange(event: ChangeEvent<HTMLInputElement>) {
    const selected = Array.from(event.target.files ?? [])
    if (selected.length > 0) {
      setResumeFiles((prev) => [
        ...prev,
        ...selected.map((file) => ({
          id: `${file.name}-${file.size}-${file.lastModified}-${Date.now()}-${Math.random().toString(36).slice(2)}`,
          file,
          label: '',
        })),
      ])
    }
    event.target.value = ''
  }

  function updateResumeFileLabel(fileID: string, label: string) {
    setResumeFiles((prev) => prev.map((item) => item.id === fileID ? { ...item, label } : item))
  }

  function removeResumeFile(fileID: string) {
    setResumeFiles((prev) => prev.filter((item) => item.id !== fileID))
  }

  async function handleResumeSubmit() {
    if (!resumePrompt.trim() && resumeFiles.length === 0) return
    await submit(async () => {
      await resumeMutation.mutateAsync()
    })
  }

  const canSubmitResume = Boolean(resumePrompt.trim() || resumeFiles.length > 0)

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
              <BreadcrumbSeparator />
              <BreadcrumbItem>
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
        <div className="flex flex-wrap items-center gap-2 lg:justify-end">
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
          {canClone && (
            <>
              <Button variant="default" size="sm" onClick={() => setShowResumeDialog(true)}>
                <Send className="h-4 w-4" />
                继续执行
              </Button>
              <Button variant="outline" size="sm" onClick={() => void handleClone()}>
                <RefreshCw className="h-4 w-4" />
                克隆任务
              </Button>
            </>
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
              <p className="mt-0.5 text-xs text-muted-foreground">该文章未发布，可修改后继续执行或克隆任务。</p>
            </div>
          </CardContent>
        </Card>
      )}

      {showWorkflowStages && (
        <WorkflowStageProgress workflow={task.workflow_status} />
      )}

      {task.status === 'completed' && (
        <WorkflowReviewSummary workflow={task.workflow_status} />
      )}

      {/* Details (stats) */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Card size="sm" className="bg-card/70">
          <CardContent>
            <p className="text-xs text-muted-foreground">创建时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.created_at)}</p>
          </CardContent>
        </Card>
        <Card size="sm" className="bg-card/70">
          <CardContent>
            <p className="text-xs text-muted-foreground">开始时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.started_at)}</p>
          </CardContent>
        </Card>
        <Card size="sm" className="bg-card/70">
          <CardContent>
            <p className="text-xs text-muted-foreground">完成时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.completed_at)}</p>
          </CardContent>
        </Card>
        <Card size="sm" className="bg-card/70">
          <CardContent>
            <p className="text-xs text-muted-foreground">来源</p>
            <p className="mt-1 text-sm text-foreground">{task.plan_id ? '计划任务' : '手动创建'}</p>
          </CardContent>
        </Card>
        {showCreditDetails && (
          <Card size="sm" className="bg-card/70">
            <CardContent className="flex items-center justify-between gap-3">
              <div>
                <p className="text-xs text-muted-foreground">积分消耗</p>
                <p className="mt-1 text-sm font-medium text-foreground">{netConsumedCredits.toLocaleString()}</p>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setShowCreditDialog(true)}>
                明细
              </Button>
            </CardContent>
          </Card>
        )}
      </div>

      {showCreditDetails && (
        <Dialog open={showCreditDialog} onOpenChange={setShowCreditDialog}>
          <DialogContent className="sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>积分明细</DialogTitle>
              <DialogDescription>本任务累计积分消耗、退还与净消耗。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-4">
                <div>
                  <p className="text-xs text-muted-foreground">积分消耗</p>
                  <p className="mt-1 text-sm font-medium text-foreground">{taskConsumedCredits.toLocaleString()}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">操作消耗</p>
                  <p className="mt-1 text-sm font-medium text-foreground">{operationConsumedCredits.toLocaleString()}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">退还积分</p>
                  <p className="mt-1 text-sm font-medium text-foreground">{refundedCredits.toLocaleString()}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">净消耗</p>
                  <p className="mt-1 text-sm font-medium text-foreground">{netConsumedCredits.toLocaleString()}</p>
                </div>
              </div>
              {creditTransactions.length > 0 && (
                <div className="overflow-x-auto border-t border-border pt-3">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="text-left text-xs text-muted-foreground">
                        <th className="py-2 pr-3 font-medium">类型</th>
                        <th className="py-2 pr-3 font-medium">数额</th>
                        <th className="py-2 pr-3 font-medium">余额</th>
                        <th className="py-2 pr-3 font-medium">描述</th>
                        <th className="py-2 font-medium">时间</th>
                      </tr>
                    </thead>
                    <tbody>
                      {creditTransactions.map((tx) => (
                        <tr key={tx.id} className="border-t border-border">
                          <td className="py-2 pr-3">
                            <Badge variant={tx.amount < 0 ? 'destructive' : 'secondary'}>
                              {transactionTypeLabel[tx.type] || tx.type}
                            </Badge>
                          </td>
                          <td className={`py-2 pr-3 font-medium ${tx.amount > 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                            {tx.amount > 0 ? '+' : ''}{tx.amount.toLocaleString()}
                          </td>
                          <td className="py-2 pr-3 text-muted-foreground">{tx.balance_after.toLocaleString()}</td>
                          <td className="max-w-[260px] truncate py-2 pr-3 text-muted-foreground">{formatCreditDescription(tx)}</td>
                          <td className="whitespace-nowrap py-2 text-xs text-muted-foreground">{formatFullDateTimeCN(tx.created_at)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </DialogContent>
        </Dialog>
      )}

      {task.type === 'seednote' && task.published && (
        <SeednoteAnalyticsPanel taskId={task.id} />
      )}

      {showProjectParameters && (
        <Card size="sm" className="border-border/70">
          <div className="flex flex-col gap-2 border-b border-border px-4 pb-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">项目参数</p>
              <h2 className="mt-1 truncate text-base font-semibold text-foreground">
                {projectParameterName}
              </h2>
            </div>
            <Badge variant="outline" className="w-fit">
              {contentTypeLabel[snapshot?.platform || project?.platform || task.type] || snapshot?.platform || project?.platform || task.type}
            </Badge>
          </div>
          <CardContent className="space-y-4">
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">视觉风格</p>
              <p className="mt-1 rounded-lg bg-muted/30 px-3 py-2 text-sm leading-6 text-foreground">
                {projectParameterVisualStyle}
              </p>
            </div>
            <div className="grid gap-3 border-t border-border pt-4 sm:grid-cols-2">
              <div>
                <p className="text-xs text-muted-foreground">图片比例</p>
                <p className="mt-1 text-sm text-foreground">{projectParameterImageRatio}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">图片模型</p>
                <p className="mt-1 break-all text-sm text-foreground">{projectParameterImageModel}</p>
              </div>
            </div>
            {task.type === 'article' && snapshot && (
              <div className="grid gap-3 border-t border-border pt-4 sm:grid-cols-3">
                <div>
                  <p className="text-xs text-muted-foreground">署名</p>
                  <p className="mt-1 text-sm text-foreground">{snapshot.author || project?.author || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">写作风格</p>
                  <p className="mt-1 text-sm text-foreground">{snapshot.writer || project?.writer || '默认'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">排版</p>
                  <p className="mt-1 text-sm text-foreground">{snapshot.theme || project?.theme || '默认'}</p>
                </div>
              </div>
            )}
            {task.type === 'ecommerce' && (snapshot?.ecommerce_defaults || project?.ecommerce_defaults) && (
              <div className="grid gap-3 border-t border-border pt-4 sm:grid-cols-2 lg:grid-cols-3">
                <div>
                  <p className="text-xs text-muted-foreground">目标平台</p>
                  <p className="mt-1 text-sm text-foreground">{snapshot?.ecommerce_defaults?.target_platform || project?.ecommerce_defaults?.target_platform || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">默认模块</p>
                  <p className="mt-1 text-sm text-foreground">
                    {(snapshot?.ecommerce_defaults?.default_selected_modules || project?.ecommerce_defaults?.default_selected_modules)
                      ? Object.entries(snapshot?.ecommerce_defaults?.default_selected_modules || project?.ecommerce_defaults?.default_selected_modules || {}).map(([k, v]) => `${k} x${v}`).join('、')
                      : '—'}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">品牌 brief</p>
                  <p className="mt-1 line-clamp-3 text-sm text-foreground">{snapshot?.ecommerce_defaults?.brand_brief || project?.ecommerce_defaults?.brand_brief || '—'}</p>
                </div>
              </div>
            )}
            {task.type === 'video' && task.video_config && (
              <div className="grid gap-3 border-t border-border pt-4 sm:grid-cols-2 lg:grid-cols-4">
                <div>
                  <p className="text-xs text-muted-foreground">视频模型</p>
                  <p className="mt-1 text-sm text-foreground">{videoModelDisplayName(task.video_config.model_key || task.video_config.model) || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">规格</p>
                  <p className="mt-1 text-sm text-foreground">{videoSpecSummary}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">估算积分</p>
                  <p className="mt-1 text-sm text-foreground">{(task.video_estimated_credits || task.video_config.estimated_credits || 0).toLocaleString()}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">积分消耗</p>
                  <p className="mt-1 text-sm text-foreground">{(task.video_credits_charged || 0).toLocaleString()}</p>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {showVideoInputParameters && (
        <Card size="sm" className="border-border/70">
          <div className="flex flex-col gap-2 border-b border-border px-4 pb-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <p className="text-xs text-muted-foreground">视频创作</p>
              <h2 className="mt-1 text-base font-semibold text-foreground">输入与创作参数</h2>
            </div>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">{videoCreativeType}</Badge>
              <Badge variant="outline">{videoPurpose}</Badge>
            </div>
          </div>
          <CardContent className="space-y-4">
            <div>
              <p className="text-xs text-muted-foreground">创作要求</p>
              <p className="mt-1 whitespace-pre-wrap rounded-lg bg-muted/30 px-3 py-2 text-sm leading-6 text-foreground">
                {task.prompt || '—'}
              </p>
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <div>
                <p className="text-xs text-muted-foreground">人物 / 主体</p>
                <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{videoSubjectProfile}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">目标受众</p>
                <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{videoAudience}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">核心信息</p>
                <p className="mt-1 whitespace-pre-wrap text-sm text-foreground">{videoSingleMessage}</p>
              </div>
            </div>
            <div className="grid gap-3 border-t border-border pt-4 sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <p className="text-xs text-muted-foreground">视频模型</p>
                <p className="mt-1 text-sm text-foreground">{videoModelDisplayName(task.video_config?.model_key || task.video_config?.model) || '—'}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">规格</p>
                <p className="mt-1 text-sm text-foreground">{videoSpecSummary}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">估算积分</p>
                <p className="mt-1 text-sm text-foreground">{(task.video_estimated_credits || task.video_config?.estimated_credits || 0).toLocaleString()}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">积分消耗</p>
                <p className="mt-1 text-sm text-foreground">{(task.video_credits_charged || task.credits_charged || 0).toLocaleString()}</p>
              </div>
            </div>
            <div className="border-t border-border pt-4">
              <p className="text-xs text-muted-foreground">参考素材</p>
              {videoInputReferences.length > 0 ? (
                <div className="mt-2 divide-y divide-border rounded-md border border-border">
                  {videoInputReferences.map((ref, index) => (
                    <div key={`${ref.type}-${ref.url || ref.text}-${index}`} className="min-w-0 px-3 py-2 text-xs">
                      <p className="truncate text-foreground">{ref.reference_role || ref.type} · {ref.file_name || ref.text || ref.url || '—'}</p>
                      {ref.input_duration_seconds ? (
                        <p className="mt-0.5 text-muted-foreground">输入时长 {ref.input_duration_seconds}s</p>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <p className="mt-1 text-xs text-muted-foreground">未使用参考素材</p>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {task.type === 'video' && videoProduction && (
        <Card size="sm" className="border-border/70">
          <CardContent>
            <VideoProductionPanel
              production={videoProduction}
              retakePending={isSubmitting}
              onRetakeAction={handleVideoRetake}
              onNextAction={(action) => toast.info(`已选择交付动作：${action}`)}
            />
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
                  <Button variant="default" size="sm" onClick={() => setShowResumeDialog(true)}>
                    <Send className="h-4 w-4" />
                    继续执行
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => void handleClone()}>
                    <RefreshCw className="h-4 w-4" />
                    克隆任务
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
                <Button variant="default" size="sm" onClick={() => setShowResumeDialog(true)}>
                  <Send className="h-4 w-4" />
                  继续执行
                </Button>
                <Button variant="outline" size="sm" onClick={() => void handleClone()}>
                  <RefreshCw className="h-4 w-4" />
                  克隆任务
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
                {/* Non-image files share the same full-width preview rows. */}
                {(() => {
                  const nonImageFiles = files.filter((f: TaskFile) => !f.mime_type?.startsWith('image/'))
                  if (nonImageFiles.length === 0) return null
                  return (
                    <div className="space-y-2">
                      <FilePreviewGallery
                        files={nonImageFiles}
                        taskId={task.id}
                        renderPreviewDetails={renderVideoPreviewDetails}
                      />
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

      <Dialog open={showResumeDialog} onOpenChange={setShowResumeDialog}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>继续执行此任务</DialogTitle>
            <DialogDescription>
              提供补充指令和文件后，任务会基于原工作目录继续执行。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <label htmlFor="resume-prompt" className="text-sm font-medium text-foreground">补充指令</label>
              <Textarea
                id="resume-prompt"
                value={resumePrompt}
                onChange={(event) => setResumePrompt(event.target.value)}
                placeholder="说明希望 AI 接着做什么，例如：基于现有草稿改成更口语化，并参考我上传的新素材。"
                className="min-h-28 resize-y"
              />
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between gap-2">
                <label htmlFor="resume-files" className="text-sm font-medium text-foreground">补充文件</label>
                <Button type="button" variant="outline" size="sm" onClick={() => resumeFileInputRef.current?.click()}>
                  <Upload className="h-4 w-4" />
                  选择文件
                </Button>
              </div>
              <Input
                ref={resumeFileInputRef}
                id="resume-files"
                type="file"
                multiple
                className="sr-only"
                onChange={handleResumeFilesChange}
              />
              {resumeFiles.length > 0 ? (
                <div className="space-y-2">
                  {resumeFiles.map((item) => (
                    <div key={item.id} className="grid gap-2 rounded-lg border border-border bg-card/50 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(180px,240px)_auto] sm:items-center">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-foreground">{item.file.name}</p>
                        <p className="text-xs text-muted-foreground">{(item.file.size / 1024).toFixed(1)} KB</p>
                      </div>
                      <div className="space-y-1">
                        <label htmlFor={`resume-file-label-${item.id}`} className="sr-only">文件说明</label>
                        <Input
                          id={`resume-file-label-${item.id}`}
                          value={item.label}
                          onChange={(event) => updateResumeFileLabel(item.id, event.target.value)}
                          placeholder="例如：客户反馈、参考图、修改意见、产品参数"
                        />
                      </div>
                      <Button type="button" variant="ghost" size="icon-sm" onClick={() => removeResumeFile(item.id)} aria-label={`移除 ${item.file.name}`}>
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-lg border border-dashed border-border px-3 py-6 text-center text-sm text-muted-foreground">
                  可选上传补充资料、修改意见或参考素材。
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setShowResumeDialog(false)}>
              取消
            </Button>
            <Button
              type="button"
              disabled={!canSubmitResume || resumeMutation.isPending}
              loading={resumeMutation.isPending}
              onClick={() => void handleResumeSubmit()}
            >
              提交并继续
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
              {task.status === 'pending' ? (
                <>此任务尚未开始执行，取消后将<strong className="text-foreground">全额退还已扣除积分</strong>，不会产生任何费用。{' '}</>
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
