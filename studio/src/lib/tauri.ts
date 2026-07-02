/**
 * Desktop (Tauri v2) bridge.
 *
 * Every export here is a NO-OP / FALSE / NULL when running in a normal web
 * browser, so the SAME studio build serves web, self-hosted, and the Tauri
 * desktop app without conditional compilation or an extra npm dependency.
 *
 * Detection: Tauri injects the full API onto `window.__TAURI__` when
 * tauri.conf.json sets `app.withGlobalTauri = true`. We deliberately do NOT
 * statically import `@tauri-apps/api` — that would force every web build to
 * carry the package. Instead all native operations are routed through custom
 * Rust IPC commands (declared in desktop/src-tauri) reached via the always-on
 * `__TAURI__.core.invoke`, and real-time events via `__TAURI__.event.listen`.
 */

// Subset of the Tauri v2 global surface this bridge touches.
interface TauriGlobal {
  core?: {
    invoke: (cmd: string, args?: Record<string, unknown>) => Promise<unknown>
  }
  event?: {
    listen: (
      event: string,
      handler: (e: { payload: unknown }) => void,
    ) => Promise<() => void>
  }
}

declare global {
  interface Window {
    __TAURI__?: TauriGlobal
  }
}

function tauri(): TauriGlobal | null {
  if (typeof window === 'undefined') return null
  return window.__TAURI__ ?? null
}

/** True when the SPA is hosted inside the Tauri desktop webview. */
export function isDesktop(): boolean {
  return tauri() !== null
}

async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T | null> {
  const t = tauri()
  if (!t?.core?.invoke) return null
  try {
    return (await t.core.invoke(cmd, args)) as T
  } catch {
    return null
  }
}

/**
 * The cloud API base the desktop targets (e.g. `https://api.anbanai.com/api/v1`),
 * or null in a browser. The Tauri shell also mirrors this into
 * `localStorage.anban_creator_api_base` via a webview initialization script so
 * http-client can resolve it synchronously before the first request; this
 * command is used by the settings UI to read/update it at runtime.
 */
export async function getApiBase(): Promise<string | null> {
  return invoke<string>('get_api_base')
}

/**
 * Persist a new cloud API base from the settings UI. Returns true on success.
 */
export async function setApiBase(base: string): Promise<boolean> {
  const ok = (await invoke<boolean>('set_api_base', { base })) ?? false
  if (ok) localStorage.setItem('anban_creator_api_base', base)
  return ok
}

/**
 * Whether a local executor (the bundled anban-creator-agent + claude runtime) is
 * provisioned and ready to claim tasks on this machine. False in a browser.
 */
export async function isLocalExecutorAvailable(): Promise<boolean> {
  return (await invoke<boolean>('is_local_executor_available')) ?? false
}

/** Whether the local executor background loop is currently active. */
export async function isLocalExecutorRunning(): Promise<boolean> {
  return (await invoke<boolean>('is_local_executor_running')) ?? false
}

/** Start the background claim loop (called when the user opts into local runs). */
export async function startLocalExecutor(): Promise<boolean> {
  return (await invoke<boolean>('start_local_executor')) ?? false
}

/** Stop the background claim loop. */
export async function stopLocalExecutor(): Promise<boolean> {
  return (await invoke<boolean>('stop_local_executor')) ?? false
}

/** Provisioning status reported by the first-run wizard backend. */
export interface LocalExecutorStatus {
  available: boolean
  running: boolean
  agent_present: boolean
  node_present: boolean
  claude_present: boolean
  plugin_present: boolean
  ffmpeg_present: boolean
  claude_authenticated: boolean
  workspace_set: boolean
  api_key_set: boolean
  workspace: string
  reason: string
}

export async function getLocalExecutorStatus(): Promise<LocalExecutorStatus | null> {
  return invoke<LocalExecutorStatus>('local_executor_status')
}

/** Persist local-executor credentials + workspace (first-run wizard / settings). */
export async function setLocalExecutorConfig(opts: {
  apiKey: string
  workspace: string
  claudeApiKey?: string
}): Promise<boolean> {
  return (await invoke<boolean>('set_local_executor_config', {
    apiKey: opts.apiKey,
    workspace: opts.workspace,
    claudeApiKey: opts.claudeApiKey ?? '',
  })) ?? false
}

/** Toggle whether the executor auto-starts on app launch. */
export async function setExecutorEnabled(enabled: boolean): Promise<boolean> {
  return (await invoke<boolean>('set_executor_enabled', { enabled })) ?? false
}

/**
 * Open a URL in the user's default system browser / app. No-op in a browser
 * (the caller should fall back to window.open for the web path).
 */
export async function openExternal(url: string): Promise<void> {
  await invoke('open_external', { url })
}

/**
 * Persist a downloaded file to disk via a native save dialog. Returns true if
 * saved, false if cancelled/failed/unavailable (browser). Bytes are passed as
 * a JSON number array — fine for documents/images; large media may warrant a
 * URL-based variant later.
 */
export async function saveBlob(filename: string, bytes: Uint8Array): Promise<boolean> {
  return (await invoke<boolean>('save_blob', { filename, data: Array.from(bytes) })) ?? false
}

/**
 * Download a blob to the user's machine. On the desktop this opens a native
 * save dialog (WKWebView ignores the usual `<a download>` trick); in a browser
 * it falls back to the blob-URL anchor click. Callers can keep one code path.
 */
export async function downloadBlob(filename: string, blob: Blob): Promise<void> {
  if (isDesktop()) {
    const bytes = new Uint8Array(await blob.arrayBuffer())
    if (await saveBlob(filename, bytes)) return
    // Save dialog cancelled or command unavailable → fall back to the web path.
  }
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/**
 * Open a URL outside the app: the system browser on desktop, a new tab in the
 * browser build. Use this anywhere the web app would call window.open(url).
 */
export async function openInExternalWindow(url: string): Promise<void> {
  if (isDesktop()) {
    await openExternal(url)
    return
  }
  window.open(url, '_blank')
}

/**
 * Native directory picker (desktop only). Returns the chosen absolute path, or
 * null when cancelled / unavailable (browser). Used to pick the workspace root.
 */
export async function pickDirectory(): Promise<string | null> {
  return (await invoke<string | null>('pick_directory')) ?? null
}

/** Event emitted by the local executor while a task runs on this machine. */
export interface LocalRunEvent {
  task_id: string
  stage?: string
  level?: 'info' | 'warn' | 'error'
  message: string
  percent?: number
}

/**
 * Subscribe to local-run progress events. Returns an unlisten function, or null
 * in a browser. The desktop emits only summaries here (full shell logs stay
 * local); cloud SSE still carries the canonical task progress.
 */
export async function onLocalRunEvent(
  handler: (e: LocalRunEvent) => void,
): Promise<(() => void) | null> {
  const t = tauri()
  if (!t?.event?.listen) return null
  try {
    return await t.event.listen('local-run://event', (e) => handler(e.payload as LocalRunEvent))
  } catch {
    return null
  }
}
