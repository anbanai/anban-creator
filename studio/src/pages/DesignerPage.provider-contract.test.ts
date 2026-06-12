import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(import.meta.dirname, '../..')

function read(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

describe('Designer provider contract', () => {
  it('sends provider_id with designer generation requests', () => {
    expect(read('src/types/designer.ts')).toContain('provider_id?: string')

    const page = read('src/pages/DesignerPage.tsx')
    expect(page).toContain('provider_id: effectiveProvider.id')
  })
})
