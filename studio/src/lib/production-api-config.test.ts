import { loadEnv } from 'vite'
import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => vi.unstubAllEnvs())

describe('production Studio API endpoint', () => {
  it('sends production requests to the independent API origin', () => {
    vi.stubEnv('VITE_API_BASE_URL', undefined)
    const env = loadEnv('production', process.cwd(), 'VITE_')
    const endpoint = new URL(env.VITE_API_BASE_URL)
    expect(endpoint.origin).toBe('https://api-creator.anbanai.com')
    expect(endpoint.pathname).toBe('/api/v1')
  })
})
