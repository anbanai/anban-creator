import { describe, expect, it, vi } from 'vitest'
import { loadWithRecovery } from './lazy-with-recovery'

function storage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  } satisfies Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
}

describe('loadWithRecovery', () => {
  it('reloads once for a failed dynamic import and rethrows the original error', async () => {
    const session = storage()
    const reload = vi.fn()
    const error = new Error('Failed to fetch dynamically imported module: /assets/LoginPage.js')

    await expect(loadWithRecovery(() => Promise.reject(error), { storage: session, reload })).rejects.toBe(error)

    expect(reload).toHaveBeenCalledOnce()
    expect(session.getItem('anban:studio:chunk-reload')).toBe('1')
  })

  it('does not reload again when the session marker is already set', async () => {
    const session = storage()
    session.setItem('anban:studio:chunk-reload', '1')
    const reload = vi.fn()
    const error = new Error('Loading chunk 42 failed.')

    await expect(loadWithRecovery(() => Promise.reject(error), { storage: session, reload })).rejects.toBe(error)

    expect(reload).not.toHaveBeenCalled()
  })

  it('clears the marker after a successful module load', async () => {
    const session = storage()
    session.setItem('anban:studio:chunk-reload', '1')

    await expect(loadWithRecovery(() => Promise.resolve({ default: 'page' }), { storage: session })).resolves.toEqual({ default: 'page' })

    expect(session.getItem('anban:studio:chunk-reload')).toBeNull()
  })

  it('rethrows ordinary module errors without reloading', async () => {
    const session = storage()
    const reload = vi.fn()
    const error = new Error('SyntaxError: Unexpected token')

    await expect(loadWithRecovery(() => Promise.reject(error), { storage: session, reload })).rejects.toBe(error)

    expect(reload).not.toHaveBeenCalled()
    expect(session.getItem('anban:studio:chunk-reload')).toBeNull()
  })
})
