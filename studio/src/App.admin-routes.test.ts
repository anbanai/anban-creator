import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const source = readFileSync(resolve(process.cwd(), 'src/App.tsx'), 'utf8')

function routeDefinition(path: string): string {
  const start = source.indexOf(`path="${path}"`)
  const end = source.indexOf('<Route', start + 1)
  return source.slice(start, end === -1 ? undefined : end)
}

describe('administrator routes', () => {
  it.each(['templates', 'designer', 'settings', 'connect/claude-code', 'connect/codex'])(
    'protects /%s with AdminRoute',
    (path) => {
      expect(routeDefinition(path)).toContain('<AdminRoute>')
    },
  )
})
