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
  it.each(['templates'])(
    'protects /%s with AdminRoute',
    (path) => {
      expect(routeDefinition(path)).toContain('<AdminRoute>')
    },
  )

  it('does not expose the removed Designer route', () => {
    expect(source).not.toContain('path="designer"')
    expect(source).not.toContain('DesignerPage')
  })

  it('keeps plugin installation public and settings available to signed-in users', () => {
    expect(routeDefinition('/plugins')).not.toContain('<ProtectedRoute>')
    expect(routeDefinition('settings')).not.toContain('<AdminRoute>')
    expect(source).toContain('<Navigate to="/plugins?client=claude" replace />')
    expect(source).toContain('<Navigate to="/plugins?client=codex" replace />')
  })
})
