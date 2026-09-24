import { lazy, type ComponentType, type LazyExoticComponent } from 'react'

export const LAZY_CHUNK_RELOAD_KEY = 'anban:studio:chunk-reload'
const RELOAD_COOLDOWN_MS = 5 * 60_000

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

interface LazyRecoveryOptions {
  storage?: StorageLike
  reload?: () => void
  onFailure?: (error: unknown) => void
  currentEntry?: string | null
  latestEntry?: () => Promise<string | null>
  now?: () => number
}

export function isDynamicImportError(error: unknown): boolean {
  if (!(error instanceof Error)) return false
  return /failed to fetch dynamically imported module|importing a module script failed|error loading dynamically imported module|loading chunk .* failed|chunkloaderror|unable to preload css/i.test(error.message)
}

function moduleEntry(document: Document): string | null {
  const src = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src')
  return src ? new URL(src, window.location.href).href : null
}

export async function latestModuleEntry(): Promise<string | null> {
  const response = await fetch('/index.html', {
    cache: 'no-store', signal: AbortSignal.timeout(5000),
  })
  if (!response.ok || response.redirected || !response.headers.get('content-type')?.includes('text/html')) return null
  return moduleEntry(new DOMParser().parseFromString(await response.text(), 'text/html'))
}

async function recover(options: LazyRecoveryOptions): Promise<void> {
  // Fail closed: if the guard cannot survive navigation, automatic reload is unsafe.
  const storage = options.storage ?? window.sessionStorage
  const currentEntry = options.currentEntry ?? moduleEntry(document)
  if (!storage || !currentEntry || navigator.onLine === false) return
  const target = await (options.latestEntry ?? latestModuleEntry)()
  if (!target || target === currentEntry) return

  // Read AFTER the network request so simultaneous import failures share the guard.
  const raw = storage.getItem(LAZY_CHUNK_RELOAD_KEY)
  const previous = raw ? JSON.parse(raw) : null
  const now = (options.now ?? Date.now)()
  if (previous && (previous.target === target || now - previous.at < RELOAD_COOLDOWN_MS)) return
  const marker = JSON.stringify({ target, at: now })
  storage.setItem(LAZY_CHUNK_RELOAD_KEY, marker)
  if (storage.getItem(LAZY_CHUNK_RELOAD_KEY) !== marker) return
  // Log the reason without URLs, query strings or user data.
  console.info('[Studio] New release detected after an asset load failure; reloading once.')
  ;(options.reload ?? (() => window.location.reload()))()
}

export async function loadWithRecovery<T>(loader: () => Promise<T>, options: LazyRecoveryOptions = {}): Promise<T> {
  try {
    return await loader()
  } catch (error) {
    try {
      options.onFailure?.(error)
      if (isDynamicImportError(error)) await recover(options)
    } catch {
      // Storage, network and diagnostic failures must never mask the original error.
    }
    throw error
  }
}

export function lazyWithRecovery<T extends ComponentType<any>>(
  loader: () => Promise<{ default: T }>,
): LazyExoticComponent<T> {
  return lazy(() => loadWithRecovery(loader))
}
