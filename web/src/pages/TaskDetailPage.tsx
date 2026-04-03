import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { TaskFile } from '@/lib/api'
import { streamTaskProgress, type SSEEvent } from '@/lib/sse'
import { useAuth } from '@/contexts/AuthContext'
import Button from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card, CardBody } from '@/components/ui/Card'
import { FilePreview } from '@/components/FilePreview'

function statusBadgeVariant(status: string) {
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
}

function formatDateTime(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export default function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { token } = useAuth()

  const [sseLogs, setSseLogs] = useState<string[]>([])
  const [sseError, setSseError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

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
      queryClient.invalidateQueries({ queryKey: ['task', id] })
    },
  })

  const connectSSE = useCallback(async () => {
    if (!id || !token) return
    const controller = new AbortController()
    abortRef.current = controller

    try {
      for await (const event of streamTaskProgress(id, token)) {
        if (controller.signal.aborted) break
        handleSSEEvent(event)
      }
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) {
        setSseError('Connection lost. Refreshing task status...')
      }
    }
  }, [id, token])

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
        setSseLogs((prev) => [...prev, `Error: ${data}`])
        break
      }
      case 'done': {
        queryClient.invalidateQueries({ queryKey: ['task', id] })
        queryClient.invalidateQueries({ queryKey: ['task-files', id] })
        setSseLogs((prev) => [...prev, '--- Task completed ---'])
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

  // Connect SSE when task is running
  useEffect(() => {
    if (task?.status === 'running' && token) {
      setSseLogs([])
      setSseError(null)
      connectSSE()
    }
    return () => {
      if (abortRef.current) {
        abortRef.current.abort()
      }
    }
  }, [task?.status, connectSSE, token])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <svg className="h-8 w-8 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      </div>
    )
  }

  if (!task) {
    return (
      <div className="text-center py-16">
        <p className="text-gray-400">Task not found</p>
        <Button variant="ghost" className="mt-3" onClick={() => navigate('/tasks')}>
          Back to Tasks
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
            className="mb-2 flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            Back to Tasks
          </button>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold text-gray-100">{task.topic}</h1>
            <Badge variant="outline">{task.type}</Badge>
            <Badge variant={statusBadgeVariant(task.status)}>{task.status}</Badge>
            {channel && (
              <Link
                to={`/channels`}
                className="flex items-center gap-1.5 rounded-md bg-gray-800 px-2 py-1 text-xs text-gray-400 transition-colors hover:bg-gray-700 hover:text-gray-200"
              >
                {channel.avatar_url ? (
                  <img src={channel.avatar_url} alt="" className="h-4 w-4 rounded-full object-cover" />
                ) : (
                  <span className="flex h-4 w-4 items-center justify-center rounded-full bg-gray-600 text-[8px] font-medium">
                    {channel.name.charAt(0)}
                  </span>
                )}
                {channel.name}
              </Link>
            )}
          </div>
        </div>
        {canCancel && (
          <Button
            variant="danger"
            size="sm"
            loading={cancelMutation.isPending}
            onClick={() => {
              if (window.confirm('Cancel this task?')) {
                cancelMutation.mutate()
              }
            }}
          >
            Cancel Task
          </Button>
        )}
      </div>

      {/* Progress bar */}
      <Card>
        <CardBody>
          {task.status === 'running' ? (
            <div>
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-gray-300">Progress</span>
                <span className="text-sm text-amber-400">{task.progress}%</span>
              </div>
              <div className="h-2 w-full rounded-full bg-gray-700">
                <div
                  className="h-2 rounded-full bg-amber-500 transition-all duration-500"
                  style={{ width: `${task.progress}%` }}
                />
              </div>
            </div>
          ) : task.status === 'completed' ? (
            <div className="flex items-center gap-2">
              <svg className="h-5 w-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
              </svg>
              <span className="text-sm text-green-400">Task completed successfully</span>
            </div>
          ) : task.status === 'failed' ? (
            <div>
              <div className="flex items-center gap-2">
                <svg className="h-5 w-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
                <span className="text-sm text-red-400">Task failed</span>
              </div>
              {task.error && (
                <p className="mt-2 rounded-lg bg-red-900/30 px-3 py-2 text-sm text-red-300">
                  {task.error}
                </p>
              )}
            </div>
          ) : task.status === 'cancelled' ? (
            <div className="flex items-center gap-2">
              <svg className="h-5 w-5 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
              <span className="text-sm text-gray-400">Task was cancelled</span>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <svg className="h-5 w-5 text-gray-500 animate-pulse" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <circle cx="12" cy="12" r="10" strokeWidth={2} />
              </svg>
              <span className="text-sm text-gray-400">Task is pending...</span>
            </div>
          )}
        </CardBody>
      </Card>

      {/* Details */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardBody>
            <p className="text-xs text-gray-500">Created</p>
            <p className="mt-1 text-sm text-gray-200">{formatDateTime(task.created_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-gray-500">Started</p>
            <p className="mt-1 text-sm text-gray-200">{formatDateTime(task.started_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-gray-500">Completed</p>
            <p className="mt-1 text-sm text-gray-200">{formatDateTime(task.completed_at)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs text-gray-500">Plan ID</p>
            <p className="mt-1 text-sm text-gray-200">{task.plan_id ?? 'Manual'}</p>
          </CardBody>
        </Card>
      </div>

      {/* Live Output / SSE Logs */}
      {task.status === 'running' && (
        <Card>
          <div className="border-b border-gray-700 px-4 py-3">
            <h2 className="text-sm font-semibold text-gray-200">Live Output</h2>
          </div>
          <div className="max-h-96 overflow-y-auto bg-gray-900 px-4 py-3">
            {sseError && (
              <p className="mb-2 text-xs text-amber-400">{sseError}</p>
            )}
            {sseLogs.length === 0 ? (
              <p className="text-xs text-gray-500">Waiting for output...</p>
            ) : (
              sseLogs.map((log, idx) => (
                <pre key={idx} className="mb-1 whitespace-pre-wrap font-mono text-xs text-gray-300">
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
          <div className="border-b border-gray-700 px-4 py-3">
            <h2 className="text-sm font-semibold text-gray-200">Result</h2>
          </div>
          <div className="max-h-64 overflow-y-auto bg-gray-900 px-4 py-3">
            <pre className="whitespace-pre-wrap font-mono text-xs text-gray-300">
              {task.result.output}
            </pre>
          </div>
        </Card>
      )}

      {/* Files */}
      {files && files.length > 0 && (
        <Card>
          <div className="border-b border-gray-700 px-4 py-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-gray-200">Generated Files ({files.length})</h2>
            <button
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
              className="px-3 py-1.5 text-xs bg-blue-600 text-white rounded hover:bg-blue-700"
            >
              Download All (ZIP)
            </button>
          </div>
          <div className="p-4 space-y-4">
            {/* Image files in grid */}
            {(() => {
              const imageFiles = files.filter((f: TaskFile) => f.mime_type?.startsWith('image/'))
              if (imageFiles.length === 0) return null
              return (
                <div>
                  <h3 className="text-xs font-medium text-gray-400 mb-2">Images</h3>
                  <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                    {imageFiles.map((file: TaskFile) => (
                      <FilePreview key={file.id} file={file} taskId={task.id} />
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
                  <h3 className="text-xs font-medium text-gray-400 mb-2">HTML Output</h3>
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
                  <h3 className="text-xs font-medium text-gray-400 mb-2">Other Files</h3>
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

      {/* No files message for completed tasks */}
      {task.status === 'completed' && (!files || files.length === 0) && (
        <Card>
          <CardBody>
            <p className="text-center text-sm text-gray-500">No files generated</p>
          </CardBody>
        </Card>
      )}
    </div>
  )
}
