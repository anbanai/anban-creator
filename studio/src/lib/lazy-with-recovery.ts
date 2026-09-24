import { lazy, type ComponentType, type LazyExoticComponent } from 'react'

export const LAZY_CHUNK_RELOAD_KEY = 'anban:studio:chunk-reload'

type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export interface LazyRecoveryOptions {
  storage?: StorageLike
  reload?: () => void
  onFailure?: (error: unknown) => void
}

export function isDynamicImportError(error: unknown): boolean {
  if (!(error instanceof Error)) return false

  return /failed to fetch dynamically imported module|importing a module script failed|loading chunk .* failed|chunkloaderror/i.test(
    error.message,
  )
}

function browserStorage(): StorageLike | undefined {
  try {
    return typeof window !== 'undefined' ? window.sessionStorage : undefined
  } catch {
    return undefined
  }
}

function browserReload(): void {
  if (typeof window !== 'undefined') window.location.reload()
}

export async function loadWithRecovery<T>(
  loader: () => Promise<T>,
  options: LazyRecoveryOptions = {},
): Promise<T> {
  const storage = options.storage ?? browserStorage()

  try {
    const result = await loader()
    storage?.removeItem(LAZY_CHUNK_RELOAD_KEY)
    return result
  } catch (error) {
    options.onFailure?.(error)
    if (!isDynamicImportError(error)) throw error

    const alreadyReloaded = storage?.getItem(LAZY_CHUNK_RELOAD_KEY) === '1'
    if (!alreadyReloaded) {
      storage?.setItem(LAZY_CHUNK_RELOAD_KEY, '1')
      ;(options.reload ?? browserReload)()
    }
    throw error
  }
}

export function lazyWithRecovery<T extends ComponentType<any>>(
  loader: () => Promise<{ default: T }>,
  options?: LazyRecoveryOptions,
): LazyExoticComponent<T> {
  return lazy(() => loadWithRecovery(loader, options))
}
