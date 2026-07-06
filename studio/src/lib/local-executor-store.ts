// Global local-executor UI state: the live-log drawer, the first-run wizard,
// the current provisioning status, and the recent local-run event stream.
//
// Why a hand-rolled store (not Zustand/Redux): the project uses TanStack Query
// for server state and local React state for the rest — see lib/command-palette.ts
// for the established useSyncExternalStore pattern this mirrors. The status pill
// (Sidebar), the live-log drawer, the first-run wizard and the root layer all
// need to read this state without prop-drilling.
//
// Selectors return one field each. Because every mutation replaces `state` with
// a new object immutably and primitives/stable-refs compare equal via Object.is
// when unchanged, a hook subscribed to e.g. `panelOpen` does NOT re-render when
// only `logs` change. Booleans are primitives (trivially cached); the `status`
// object and `logs` array are replaced only on change, so their references are
// stable between mutations.

import { useSyncExternalStore } from 'react'
import type { LocalExecutorStatus, LocalRunEvent } from '@/lib/tauri'

export type LogLevel = 'info' | 'warn' | 'error'

export interface LogEntry {
  id: number
  ts: number
  taskId?: string
  stage?: string
  level: LogLevel
  message: string
}

interface State {
  /** Live-log drawer open. */
  panelOpen: boolean
  /** First-run wizard open. */
  wizardOpen: boolean
  /** Session-scoped: once the user dismisses the wizard, don't auto-open again
   *  this session (they can still reopen it manually from the status pill). */
  wizardDismissed: boolean
  /** Latest provisioning snapshot from the Rust shell (null in a browser). */
  status: LocalExecutorStatus | null
  /** Recent local-run events, newest last. Capped to bound memory. */
  logs: LogEntry[]
}

const MAX_LOGS = 300

const initialState: State = {
  panelOpen: false,
  wizardOpen: false,
  wizardDismissed: false,
  status: null,
  logs: [],
}

let state: State = initialState
const listeners = new Set<() => void>()
let nextId = 1

function emit() {
  for (const l of listeners) l()
}

function set(patch: Partial<State>) {
  state = { ...state, ...patch }
  emit()
}

export const localExecutorStore = {
  subscribe(listener: () => void): () => void {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },

  // --- live-log drawer ---
  openPanel() {
    set({ panelOpen: true })
  },
  closePanel() {
    set({ panelOpen: false })
  },
  togglePanel() {
    set({ panelOpen: !state.panelOpen })
  },

  // --- first-run wizard ---
  openWizard() {
    set({ wizardOpen: true })
  },
  closeWizard() {
    set({ wizardOpen: false })
  },
  /** Dismiss + suppress auto-open for the rest of the session. */
  dismissWizard() {
    set({ wizardOpen: false, wizardDismissed: true })
  },

  // --- status + logs (written by the root LocalExecutorLayer) ---
  setStatus(status: LocalExecutorStatus | null) {
    set({ status })
  },
  pushLog(e: LocalRunEvent) {
    const entry: LogEntry = {
      id: nextId++,
      ts: Date.now(),
      taskId: e.task_id,
      stage: e.stage,
      level: e.level ?? 'info',
      message: e.message,
    }
    const base = state.logs
    const logs = base.length >= MAX_LOGS ? base.slice(base.length - MAX_LOGS + 1) : base.slice()
    logs.push(entry)

    const status = state.status ? { ...state.status } : null
    if (status) {
      status.last_event_at = new Date(entry.ts).toISOString()
      if (entry.level === 'error') {
        status.state = 'error'
        status.last_error = entry.message
      } else if (entry.message.startsWith('本地执行器已启动')) {
        status.state = 'running_idle'
        status.running = true
        status.last_error = null
      } else if (entry.message.startsWith('已认领任务')) {
        status.state = 'running_task'
        status.running = true
        status.current_task_id = entry.taskId ?? extractTaskId(entry.message)
      } else if (entry.message.includes('agent exited')) {
        status.state = status.running ? 'running_idle' : 'ready_stopped'
        status.current_task_id = null
      } else if (entry.message.startsWith('本地执行器已停止')) {
        status.state = status.available ? 'ready_stopped' : 'needs_config'
        status.running = false
        status.current_task_id = null
      }
    }
    set({ logs, status })
  },
  clearLogs() {
    set({ logs: [] })
  },

  // --- selectors (one field each) ---
  getPanelOpen: () => state.panelOpen,
  getWizardOpen: () => state.wizardOpen,
  getWizardDismissed: () => state.wizardDismissed,
  getStatus: () => state.status,
  getLogs: () => state.logs,
}

function extractTaskId(message: string): string | null {
  const match = message.match(/任务\s+([^\s（]+)/)
  return match?.[1] ?? null
}

// --- React hooks (each subscribes to a single slice) ---

export function useLocalExecutorPanelOpen(): boolean {
  return useSyncExternalStore(localExecutorStore.subscribe, localExecutorStore.getPanelOpen)
}

export function useLocalExecutorWizardOpen(): boolean {
  return useSyncExternalStore(localExecutorStore.subscribe, localExecutorStore.getWizardOpen)
}

export function useLocalExecutorWizardDismissed(): boolean {
  return useSyncExternalStore(localExecutorStore.subscribe, localExecutorStore.getWizardDismissed)
}

export function useLocalExecutorStatus(): LocalExecutorStatus | null {
  return useSyncExternalStore(localExecutorStore.subscribe, localExecutorStore.getStatus)
}

export function useLocalExecutorLogs(): LogEntry[] {
  return useSyncExternalStore(localExecutorStore.subscribe, localExecutorStore.getLogs)
}
