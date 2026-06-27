import { ref, watch, onUnmounted } from 'vue'
import { tasksApi } from '@/api/tasks'
import { useTaskStream, type TaskStreamEvent } from '@/composables/useTaskStream'
import type { Task, TaskStatus } from '@/types'

const TERMINAL: TaskStatus[] = ['completed', 'failed', 'cancelled']

/**
 * Reactive task state with real-time updates.
 *
 * Transport is chosen by `useTaskStream` via feature detection: SSE
 * (fetch + ReadableStream) on H5/browsers, polling on mp-weixin. The public
 * interface (task/logs/progress/status/progressMessage/polling/startPolling/
 * stopPolling) is unchanged from the old pure-polling version, so consumers
 * like tasks/detail.vue get real-time updates for free.
 */
export function usePolling() {
  const task = ref<Task | null>(null)
  const logs = ref<string[]>([])
  const progress = ref(0)
  const status = ref<string>('')
  const progressMessage = ref('')
  const polling = ref(false)

  const taskId = ref('')
  let lastLogLength = 0

  function applyTask(t: Task) {
    task.value = t
    status.value = t.status
    // Monotonic progress: max(live latest_progress, persisted, current).
    const live = t.latest_progress?.percent ?? 0
    const persisted = t.progress ?? 0
    progress.value = Math.max(0, Math.min(100, Math.max(progress.value, live, persisted)))

    if (t.progress_log) {
      const allLogs = t.progress_log.split('\n').filter(Boolean)
      if (allLogs.length > lastLogLength) {
        logs.value = [...logs.value, ...allLogs.slice(lastLogLength)]
        lastLogLength = allLogs.length
      }
      if (allLogs.length > 0) {
        progressMessage.value = allLogs[allLogs.length - 1]
      }
    }
  }

  function applyProgress(p: {
    stage?: string
    title?: string
    description?: string
    percent?: number
  }) {
    const t = task.value
    if (!t) return
    if (!t.latest_progress) t.latest_progress = {}
    if (p.stage !== undefined) t.latest_progress.stage = p.stage
    if (p.title !== undefined) t.latest_progress.title = p.title
    if (p.description !== undefined) t.latest_progress.description = p.description
    if (typeof p.percent === 'number') {
      const next = Math.max(0, Math.min(100, p.percent))
      progress.value = Math.max(progress.value, next)
    }
    if (p.title || p.description) {
      progressMessage.value = p.title || p.description || progressMessage.value
    }
  }

  function appendLog(line: string) {
    if (!line) return
    logs.value = [...logs.value, line]
    progressMessage.value = line
  }

  function applyStatus(s: TaskStatus) {
    status.value = s
    if (task.value) task.value.status = s
  }

  async function refreshTask(id: string): Promise<Task | null> {
    try {
      const t = await tasksApi.get(id)
      applyTask(t)
      return t
    } catch (err) {
      console.error('Failed to load task:', err)
      return null
    }
  }

  function onEvent(e: TaskStreamEvent) {
    switch (e.kind) {
      case 'task':
        applyTask(e.task)
        break
      case 'progress':
        applyProgress(e)
        break
      case 'log':
        appendLog(e.line)
        break
      case 'status':
        applyStatus(e.status)
        break
    }
  }

  const { connected, transport, start: startStream, stop: stopStream } = useTaskStream(taskId, {
    onEvent,
    onTerminal: () => {
      // Terminal state reached. The SSE path only delivered a terminal status
      // event (no full task body), so capture completed_at / final state with
      // one fetch. Capture the transport BEFORE stopping — stopStream() resets
      // it to 'idle'.
      const needFinalFetch = transport.value === 'sse'
      // Stop the stream so it doesn't reconnect (SSE) for a finished task and
      // the "live" indicator turns off (both transports: stopStream flips
      // connected→false → the watch sets polling=false).
      stopStream()
      if (needFinalFetch && taskId.value) {
        void refreshTask(taskId.value)
      }
    },
    onError: (err) => console.error('Task stream error:', err),
  })

  // Mirror connection state onto the `polling` flag for UI affordances.
  watch(connected, (c) => {
    polling.value = c
  })

  async function startPolling(id: string) {
    stopPolling()
    taskId.value = id
    lastLogLength = 0
    logs.value = []
    progress.value = 0
    polling.value = true

    // Seed the full task immediately (covers both transports and shows data
    // before the SSE connection opens).
    const t = await refreshTask(id)
    if (t && TERMINAL.includes(t.status)) {
      polling.value = false
      return
    }

    // Start the live stream: SSE on H5, polling on mp-weixin.
    startStream()
  }

  function stopPolling() {
    stopStream()
    polling.value = false
  }

  onUnmounted(() => {
    stopPolling()
  })

  return {
    task,
    logs,
    progress,
    status,
    progressMessage,
    polling,
    startPolling,
    stopPolling,
    // Transport in use: 'sse' | 'poll' | 'idle' (optional, for UI).
    transport,
  }
}
