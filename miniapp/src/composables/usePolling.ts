import { ref, onUnmounted } from 'vue'
import { tasksApi } from '@/api/tasks'
import { POLL_INTERVAL_RUNNING, POLL_INTERVAL_PENDING } from '@/utils/constants'
import type { Task } from '@/types'

export function usePolling() {
  const task = ref<Task | null>(null) as { value: Task | null }
  const logs = ref<string[]>([])
  const progress = ref(0)
  const status = ref<string>('')
  const progressMessage = ref('')
  const polling = ref(false)
  let timer: ReturnType<typeof setInterval> | null = null
  let lastLogLength = 0

  function startPolling(taskId: string) {
    stopPolling()
    polling.value = true
    lastLogLength = 0
    poll(taskId)
  }

  async function poll(taskId: string) {
    try {
      const t = await tasksApi.get(taskId)
      task.value = t
      status.value = t.status
      progress.value = t.progress ?? 0

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

      // Terminal states — stop polling
      if (['completed', 'failed', 'cancelled'].includes(t.status)) {
        stopPolling()
        return
      }

      // Schedule next poll
      const interval = t.status === 'running'
        ? POLL_INTERVAL_RUNNING
        : POLL_INTERVAL_PENDING

      timer = setTimeout(() => poll(taskId), interval)
    } catch (err) {
      console.error('Polling error:', err)
      // Retry after a longer interval on error
      timer = setTimeout(() => poll(taskId), 10000)
    }
  }

  function stopPolling() {
    polling.value = false
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
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
  }
}
