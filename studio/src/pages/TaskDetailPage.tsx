import { useState, useEffect, useRef } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ArrowLeft, Check, X, Circle, Loader2, Download, Eye } from 'lucide-react'
import { api } from '@/lib/api'
import type { TaskFile } from '@/lib/api'
import { streamTaskProgress, type SSEEvent } from '@/lib/sse'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card, CardBody } from '@/components/ui/Card'
import { FilePreview } from '@/components/FilePreview'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { taskStatusLabel, contentTypeLabel, formatFullDateTimeCN, statusBadgeVariant } from '@/lib/labels'

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
  const [showCancelDialog, setShowCancelDialog] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const tokenRef = useRef(token)
  tokenRef.current = token

  const { data: task, isLoading } = useQuery({
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

  // Resolve channel info for the task
  const { data: channelDetail } = useQuery({
    queryKey: ['channel', task?.channel_id],
    queryFn: () => api.channels.get(task!.channel_id),
    enabled: !!task?.channel_id,
  })
  const channel = channelDetail?.channel

  const cancelMutation = useMutation({
    mutationFn: () => api.tasks.cancel(id!),
    onSuccess: () => {
      toast.success('任务已取消')
      abortRef.current?.abort()
      abortRef.current = null
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      setShowCancelDialog(false)
    },
  })

  const togglePublished = useMutation({
    mutationFn: ({ published }: { published: boolean }) =>
      api.tasks.markPublished(id!, published),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task', id] })
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
        setSseLogs((prev) => [...prev, `连接断开，正在重试 (${retries + 1}/3)...`])
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
        const data = parsed as { progress: number; message: string }
        setSseLogs((prev) => [...prev, `[${data.progress}%] ${data.message}`])
        break
      }
      case 'output': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { text?: string }).text || ''
        if (data) {
          setSseLogs((prev) => [...prev, data])
        }
        break
      }
      case 'error': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { error?: string }).error || 'Unknown error'
        setSseLogs((prev) => [...prev, `错误：${data}`])
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
        setSseLogs((prev) => [...prev, `--- ${statusText} ---`])
        break
      }
      default: {
        const text = typeof parsed === 'string' ? parsed : JSON.stringify(parsed)
        if (text && text !== '{}' && text.length > 0) {
          setSseLogs((prev) => [...prev, text])
        }
      }
    }
  }

  // Connect SSE only when task status transitions to "running"
  useEffect(() => {
    if (task?.status === 'running') {
      setSseLogs([])
      setSseError(null)
      connectSSE()
    }
    return () => {
      if (abortRef.current) {
        abortRef.current.abort()
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task?.status])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
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

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <button
            onClick={() => navigate('/tasks')}
            className="mb-2 flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="h-4 w-4" />
            返回任务列表
          </button>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold text-foreground">{task.topic}</h1>
            <Badge variant="outline">{contentTypeLabel[task.type] || task.type}</Badge>
            <Badge variant={statusBadgeVariant(task.status)}>{taskStatusLabel[task.status] || task.status}</Badge>
            {channel && (
              <Link
                to={`/channels`}
                className="flex items-center gap-1.5 rounded-md bg-card px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              >
                {channel.avatar_url ? (
                  <img src={channel.avatar_url} alt="" className="h-4 w-4 rounded-full object-cover" />
                ) : (
                  <span className="flex h-4 w-4 items-center justify-center rounded-full bg-secondary text-[8px] font-medium">
                    {channel.name.charAt(0)}
                  </span>
                )}
                {channel.name}
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
              onClick={() => togglePublished.mutate({ published: !task.published })}
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
        </div>
      </div>

      {/* Details (stats) */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardBody>
            <p className="text-xs text-muted-foreground">创建时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.created_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-muted-foreground">开始时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.started_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-muted-foreground">完成时间</p>
            <p className="mt-1 text-sm text-foreground">{formatFullDateTimeCN(task.completed_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-muted-foreground">来源</p>
            <p className="mt-1 text-sm text-foreground">{task.plan_id ? '计划任务' : '手动创建'}</p>
          </CardBody>
        </Card>
      </div>

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
                  console.error('Failed to download ZIP:', err)
                }
              }}
            >
              <Download className="h-4 w-4" />
              下载全部 (ZIP)
            </Button>
          </div>
          <div className="p-4 space-y-4">
            {/* Image files in compact grid */}
            {(() => {
              const imageFiles = files.filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
              if (imageFiles.length === 0) return null
              return (
                <div>
                  <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
                    {imageFiles.map((file: TaskFile) => (
                      <div key={file.id} className="shrink-0 snap-start">
                        <FilePreview file={file} taskId={task.id} />
                      </div>
                    ))}
                  </div>
                </div>
              )
            })()}
            {/* HTML files */}
            {(() => {
              const htmlFiles = files.filter((f: TaskFile) => f.mime_type === 'text/html')
              if (htmlFiles.length === 0) return null
              return (
                <div>
                  <div className="space-y-3">
                    {htmlFiles.map((file: TaskFile) => (
                      <FilePreview key={file.id} file={file} taskId={task.id} />
                    ))}
                  </div>
                </div>
              )
            })()}
            {/* Other files */}
            {(() => {
              const otherFiles = files.filter((f: TaskFile) => !f.mime_type?.startsWith('image/') && f.mime_type !== 'text/html')
              if (otherFiles.length === 0) return null
              return (
                <div>
                  <div className="space-y-2">
                    {otherFiles.map((file: TaskFile) => (
                      <FilePreview key={file.id} file={file} taskId={task.id} />
                    ))}
                  </div>
                </div>
              )
            })()}
          </div>
        </Card>
      )}

      {/* Progress bar */}
      <Card>
        <CardBody>
          {task.status === 'running' ? (
            <div>
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-foreground">进度</span>
                <span className="text-sm text-primary">{task.progress}%</span>
              </div>
              <div className="h-2 w-full rounded-full bg-muted">
                <div
                  className="h-2 rounded-full bg-primary transition-all duration-500"
                  style={{ width: `${task.progress}%` }}
                />
              </div>
            </div>
          ) : task.status === 'completed' ? (
            <div className="flex items-center gap-2">
              <Check className="h-5 w-5 text-emerald-400" />
              <span className="text-sm text-emerald-400">任务执行成功</span>
            </div>
          ) : task.status === 'failed' ? (
            <div>
              <div className="flex items-center gap-2">
                <X className="h-5 w-5 text-red-400" />
                <span className="text-sm text-red-400">任务失败</span>
              </div>
              {task.error && (
                <p className="mt-2 bg-red-900/20 border border-red-900/30 rounded-lg px-3 py-2 text-sm text-red-300">
                  {task.error}
                </p>
              )}
            </div>
          ) : task.status === 'cancelled' ? (
            <div className="flex items-center gap-2">
              <X className="h-5 w-5 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">任务已取消</span>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <Circle className="h-5 w-5 text-muted-foreground animate-pulse" />
              <span className="text-sm text-muted-foreground">任务等待执行中...</span>
            </div>
          )}
        </CardBody>
      </Card>

      {/* Live Output / SSE Logs */}
      {task.status === 'running' && (
        <Card>
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold text-foreground">执行日志</h2>
          </div>
          <div className="max-h-96 overflow-y-auto bg-background/50 rounded-lg border border-border font-mono px-4 py-3">
            {sseError && (
              <p className="mb-2 text-xs text-amber-400">{sseError}</p>
            )}
            {sseLogs.length === 0 ? (
              <p className="text-xs text-muted-foreground">等待输出中...</p>
            ) : (
              sseLogs.map((log, idx) => (
                <pre key={idx} className="mb-1 whitespace-pre-wrap text-xs text-foreground">
                  {log}
                </pre>
              ))
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
          <div className="max-h-64 overflow-y-auto bg-background/50 rounded-lg border border-border px-4 py-3 prose prose-invert prose-sm max-w-none">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {task.result.output}
            </ReactMarkdown>
          </div>
        </Card>
      )}

      {/* Cancel confirmation */}
      <AlertDialog open={showCancelDialog} onOpenChange={setShowCancelDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定取消此任务？</AlertDialogTitle>
            <AlertDialogDescription>取消后任务将停止执行，此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>再想想</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => cancelMutation.mutate()}>
              确定取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
