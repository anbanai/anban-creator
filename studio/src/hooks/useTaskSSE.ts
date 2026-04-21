import { useState, useEffect, useRef, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { streamTaskProgress, type SSEEvent } from '@/lib/sse'

export interface UseTaskSSEReturn {
  logs: string[]
  error: string | null
  isConnected: boolean
  abort: () => void
}

interface UseTaskSSEOptions {
  taskId: string | undefined
  token: string | null | undefined
  enabled: boolean
}

/**
 * Hook that manages SSE connection for task progress streaming.
 * Owns AbortController lifecycle and properly cleans up on unmount.
 */
export function useTaskSSE({ taskId, token, enabled }: UseTaskSSEOptions): UseTaskSSEReturn {
  const [logs, setLogs] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const tokenRef = useRef(token)
  const enabledRef = useRef(enabled)
  const queryClient = useQueryClient()

  tokenRef.current = token
  enabledRef.current = enabled

  const abort = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    setIsConnected(false)
  }, [])

  const handleSSEEvent = useCallback((event: SSEEvent) => {
    const parsed = typeof event.data === 'string'
      ? (() => { try { return JSON.parse(event.data) } catch { return event.data } })()
      : event.data

    switch (event.event) {
      case 'progress': {
        if (typeof parsed === 'string') {
          setLogs((prev) => [...prev, parsed])
        } else {
          const data = parsed as { progress: number; message: string }
          if (data.progress != null) {
            setLogs((prev) => [...prev, `[${data.progress}%] ${data.message}`])
          }
        }
        break
      }
      case 'output': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { text?: string }).text || ''
        if (data) {
          setLogs((prev) => [...prev, data])
        }
        break
      }
      case 'error': {
        const data = typeof parsed === 'string' ? parsed : (parsed as { error?: string }).error || 'Unknown error'
        setLogs((prev) => [...prev, `错误：${data}`])
        break
      }
      case 'done':
      case 'completed':
      case 'failed':
      case 'cancelled': {
        if (taskId) {
          queryClient.invalidateQueries({ queryKey: ['task', taskId] })
          queryClient.invalidateQueries({ queryKey: ['task-files', taskId] })
        }
        const statusText = event.event === 'completed' ? '任务完成'
          : event.event === 'failed' ? '任务失败'
            : event.event === 'cancelled' ? '任务取消' : '任务完成'
        setLogs((prev) => [...prev, `--- ${statusText} ---`])
        break
      }
      default: {
        const text = typeof parsed === 'string' ? parsed : JSON.stringify(parsed)
        if (text && text !== '{}' && text.length > 0) {
          setLogs((prev) => [...prev, text])
        }
      }
    }
  }, [taskId, queryClient])

  const connectSSE = useCallback(async (retries = 0) => {
    if (!taskId) return
    const currentToken = tokenRef.current
    if (!currentToken) return
    if (!enabledRef.current) return

    // Abort any existing connection
    if (abortRef.current) {
      abortRef.current.abort()
    }
    const controller = new AbortController()
    abortRef.current = controller

    try {
      setIsConnected(true)
      setError(null)
      for await (const event of streamTaskProgress(taskId, currentToken, controller.signal)) {
        handleSSEEvent(event)
      }
    } catch (err) {
      setIsConnected(false)
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (retries < 3 && !controller.signal.aborted) {
        setLogs((prev) => [...prev, `连接断开，正在重试 (${retries + 1}/3)...`])
        await new Promise((r) => setTimeout(r, 2000 * (retries + 1)))
        if (controller.signal.aborted) return
        return connectSSE(retries + 1)
      }
      setError('连接断开，正在刷新任务状态...')
      if (taskId) {
        queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      }
    }
  }, [taskId, handleSSEEvent, queryClient])

  useEffect(() => {
    if (enabled && taskId) {
      setLogs([])
      setError(null)
      connectSSE()
    }
    return () => {
      // Cleanup on unmount: abort without reconnection
      if (abortRef.current) {
        abortRef.current.abort()
        abortRef.current = null
      }
    }
    // Only connect when enabled changes (i.e., when task status becomes 'running')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled])

  return { logs, error, isConnected, abort }
}
