// Unified real-time task stream for the miniapp.
//
// The server pushes task progress via SSE at GET /api/v1/tasks/:id/stream (the
// same endpoint studio consumes). Browsers/H5 can stream it with fetch +
// ReadableStream (supports the Authorization header, unlike EventSource).
// mp-weixin has no fetch streaming, so it falls back to polling GET
// /api/v1/tasks/:id.
//
// Transport is chosen by feature detection at runtime (progressive
// enhancement): if fetch + ReadableStream exist, try SSE; on any SSE failure,
// degrade to polling. No conditional compilation needed — the same code path
// runs everywhere and self-heals.

import { ref, onUnmounted, type Ref } from 'vue'
import { tasksApi } from '@/api/tasks'
import type { Task, TaskStatus } from '@/types'

export type TaskStreamEvent =
  | {
      kind: 'progress'
      stage?: string
      title?: string
      description?: string
      percent?: number
    }
  | { kind: 'log'; line: string }
  | { kind: 'status'; status: TaskStatus }
  | { kind: 'task'; task: Task }

export interface UseTaskStreamOptions {
  /** Called for every event (progress, log, status, or full task refresh). */
  onEvent?: (e: TaskStreamEvent) => void
  /** Called once when the task reaches a terminal status. */
  onTerminal?: (status: TaskStatus) => void
  /** Called on transport errors (non-fatal — polling fallback kicks in). */
  onError?: (err: unknown) => void
  /** Polling interval in ms (default 2000). */
  pollInterval?: number
}

const TERMINAL: TaskStatus[] = ['completed', 'failed', 'cancelled']

function canStream(): boolean {
  return typeof fetch === 'function' && typeof ReadableStream !== 'undefined'
}

export function useTaskStream(taskId: Ref<string>, opts: UseTaskStreamOptions = {}) {
  const connected = ref(false)
  const transport = ref<'sse' | 'poll' | 'idle'>('idle')

  let stopped = false
  let pollTimer: ReturnType<typeof setTimeout> | null = null
  let abortController: AbortController | null = null
  const pollInterval = opts.pollInterval ?? 2000

  function emit(e: TaskStreamEvent) {
    opts.onEvent?.(e)
    if (e.kind === 'status' && TERMINAL.includes(e.status)) {
      opts.onTerminal?.(e.status)
    }
  }

  // ---- Polling: mp-weixin default + SSE fallback ----
  async function pollOnce(id: string) {
    try {
      const task = await tasksApi.get(id)
      emit({ kind: 'task', task })
      if (TERMINAL.includes(task.status)) {
        opts.onTerminal?.(task.status)
        return
      }
    } catch (err) {
      opts.onError?.(err)
    }
    if (!stopped) {
      pollTimer = setTimeout(() => pollOnce(id), pollInterval)
    }
  }

  function startPolling(id: string) {
    transport.value = 'poll'
    connected.value = true
    pollOnce(id)
  }

  function stopPolling() {
    if (pollTimer) {
      clearTimeout(pollTimer)
      pollTimer = null
    }
  }

  // ---- SSE: H5 / browsers with fetch streaming ----
  async function startSSE(id: string) {
    const token = uni.getStorageSync('anban_creator_token')
    if (!token) {
      startPolling(id)
      return
    }
    abortController = new AbortController()
    transport.value = 'sse'
    connected.value = true
    try {
      const sse = await import('@/utils/sse')
      for await (const ev of sse.streamTaskProgress(id, token, abortController.signal)) {
        if (stopped) break
        handleSSE(ev)
      }
      // Stream ended (server closes after its 30-min timeout, or on terminal).
      // If we're still active and haven't hit a terminal status, reconnect to
      // catch any late events.
      if (!stopped) {
        pollTimer = setTimeout(() => {
          if (!stopped) void startSSE(id)
        }, 1500)
      }
    } catch (err: unknown) {
      if (stopped) return
      const name = (err as { name?: string })?.name
      if (name === 'AbortError') return
      opts.onError?.(err)
      // Degrade to polling so the user still sees updates.
      stopPolling()
      startPolling(id)
    }
  }

  function handleSSE(ev: { event: string; data: string }) {
    const type = ev.event || 'progress'
    let payload: unknown
    try {
      payload = ev.data ? JSON.parse(ev.data) : null
    } catch {
      payload = ev.data
    }

    if (type === 'progress') {
      if (payload && typeof payload === 'object') {
        const p = payload as {
          stage?: string
          title?: string
          description?: string
          percent?: number
        }
        emit({
          kind: 'progress',
          stage: p.stage,
          title: p.title,
          description: p.description,
          percent: p.percent,
        })
      } else if (typeof payload === 'string') {
        emit({ kind: 'log', line: payload })
      }
    } else if (type === 'timeout') {
      // Server closed the long-lived connection; startSSE loop reconnects.
    } else if (TERMINAL.includes(type as TaskStatus)) {
      emit({ kind: 'status', status: type as TaskStatus })
    }
  }

  // ---- Public API ----
  function start() {
    const id = taskId.value
    if (!id) return
    stopped = false
    if (canStream()) {
      void startSSE(id)
    } else {
      startPolling(id)
    }
  }

  function stop() {
    stopped = true
    stopPolling()
    if (abortController) {
      abortController.abort()
      abortController = null
    }
    connected.value = false
    transport.value = 'idle'
  }

  // Restart the stream when the watched task id changes.
  // NOTE: lifecycle is caller-driven (start()/stop()). The wrapper composable
  // (usePolling) sets taskId then calls start() explicitly for deterministic
  // control, so there is no auto-watch here.

  onUnmounted(stop)

  return { connected, transport, start, stop }
}
