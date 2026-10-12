import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('production Studio API endpoint', () => {
  it('uses the same-origin API gateway instead of the retired API subdomain', () => {
    const env = readFileSync(resolve(process.cwd(), '.env.production'), 'utf8')

    expect(env).toContain('VITE_API_BASE_URL=/api/v1')
    expect(env).not.toContain('api.creator.anbanai.com')
  })
})
