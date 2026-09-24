import { afterEach, describe, expect, it, vi } from 'vitest'
import { loadWithRecovery } from './lazy-with-recovery'

function storage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}
const failure = new TypeError('Failed to fetch dynamically imported module: /assets/LoginPage-old.js')
const fail = () => Promise.reject(failure)
const currentEntry = 'https://studio.test/assets/index-old.js'
function options() {
  return {
    storage: storage(), reload: vi.fn(), currentEntry,
    latestEntry: async (): Promise<string | null> => 'https://studio.test/assets/index-new.js',
    now: () => 1_000_000, onFailure: vi.fn(),
  }
}
afterEach(() => { vi.restoreAllMocks() })

describe('loadWithRecovery', () => {
  it('reloads once only after confirming a different release', async () => {
    const opts = options()
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    expect(opts.reload).toHaveBeenCalledOnce()
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    expect(opts.reload).toHaveBeenCalledOnce()
  })
  it('does not reset the reload guard when another route succeeds', async () => {
    const opts = options()
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    await expect(loadWithRecovery(async () => 'dashboard', opts)).resolves.toBe('dashboard')
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    expect(opts.reload).toHaveBeenCalledOnce()
  })
  it.each(['same', 'unreachable', 'missing'] as const)('does not refresh if the release is %s', async (state) => {
    const opts = options()
    const latestEntry = async () => {
      if (state === 'unreachable') throw new Error('offline')
      return state === 'same' ? currentEntry : null
    }
    await expect(loadWithRecovery(fail, { ...opts, latestEntry })).rejects.toBe(failure)
    expect(opts.reload).not.toHaveBeenCalled()
  })
  it.each(['getItem', 'setItem'] as const)('preserves the import error and never reloads if storage.%s fails', async (method) => {
    const opts = options()
    opts.storage[method] = () => { throw new Error('Storage denied') }
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    expect(opts.reload).not.toHaveBeenCalled()
  })
  it('does not reload when the browser denies access to sessionStorage', async () => {
    const opts = options()
    vi.spyOn(window, 'sessionStorage', 'get').mockImplementation(() => { throw new Error('denied') })
    await expect(loadWithRecovery(fail, { ...opts, storage: undefined })).rejects.toBe(failure)
    expect(opts.reload).not.toHaveBeenCalled()
  })
  it('does not reload when storage silently fails to persist the guard', async () => {
    const opts = options()
    opts.storage.setItem = () => {}
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    expect(opts.reload).not.toHaveBeenCalled()
  })
  it('keeps successful imports successful even when storage is broken', async () => {
    const opts = options()
    opts.storage.removeItem = () => { throw new Error('denied') }
    await expect(loadWithRecovery(async () => 'page', opts)).resolves.toBe('page')
  })
  it('deduplicates simultaneous route failures', async () => {
    const opts = options()
    await Promise.allSettled([loadWithRecovery(fail, opts), loadWithRecovery(fail, opts)])
    expect(opts.reload).toHaveBeenCalledOnce()
  })
  it('blocks alternating release responses but permits a later release', async () => {
    const opts = options()
    await expect(loadWithRecovery(fail, opts)).rejects.toBe(failure)
    const next = { ...opts, latestEntry: async () => 'https://studio.test/assets/index-next.js' }
    await expect(loadWithRecovery(fail, next)).rejects.toBe(failure)
    expect(opts.reload).toHaveBeenCalledOnce()
    await expect(loadWithRecovery(fail, { ...next, now: () => 2_000_000 })).rejects.toBe(failure)
    expect(opts.reload).toHaveBeenCalledTimes(2)
  })
  it.each([
    'Importing a module script failed.',
    'error loading dynamically imported module: /assets/a.js',
    'Unable to preload CSS for /assets/page.css',
  ])('recognizes browser/Vite load error: %s', async (message) => {
    const opts = options()
    const error = new Error(message)
    await expect(loadWithRecovery(() => Promise.reject(error), opts)).rejects.toBe(error)
    expect(opts.reload).toHaveBeenCalledOnce()
  })
  it('preserves ordinary errors without refresh or version request', async () => {
    const opts = options()
    const latestEntry = vi.fn(opts.latestEntry)
    const error = new SyntaxError('Unexpected token')
    await expect(loadWithRecovery(() => Promise.reject(error), { ...opts, latestEntry })).rejects.toBe(error)
    expect(opts.reload).not.toHaveBeenCalled()
    expect(latestEntry).not.toHaveBeenCalled()
  })
  it('preserves the original failure when diagnostics throw', async () => {
    const opts = options()
    await expect(loadWithRecovery(fail, { ...opts, onFailure: () => { throw new Error('logger') } })).rejects.toBe(failure)
  })
})

describe('release detection over HTTP', () => {
  it('reads the new HTML entry with caching disabled and a bounded request', async () => {
    const { latestModuleEntry } = await import('./lazy-with-recovery')
    const request = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      '<script type="module" crossorigin src="/assets/index-new.js"></script>',
      { headers: { 'Content-Type': 'text/html' } },
    ))
    expect(await latestModuleEntry()).toBe(new URL('/assets/index-new.js', window.location.href).href)
    expect(request).toHaveBeenCalledWith('/index.html', expect.objectContaining({ cache: 'no-store', signal: expect.any(AbortSignal) }))
  })
  it.each([
    [503, 'text/html', '<script type="module" src="/assets/new.js"></script>'],
    [200, 'application/json', '{}'],
    [200, 'text/html', '<h1>Proxy error</h1>'],
  ])('ignores unusable HTML (%s, %s)', async (status, contentType, body) => {
    const { latestModuleEntry } = await import('./lazy-with-recovery')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(body, { status, headers: { 'Content-Type': contentType } }))
    expect(await latestModuleEntry()).toBeNull()
  })
})
