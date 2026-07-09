import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('montage UX contracts', () => {
  it('tasks page uses dedicated montage input and no user execution target selector', () => {
    const source = readFileSync('src/pages/TasksPage.tsx', 'utf8')
    expect(source).toContain('montage_input')
    expect(source).toContain('MontageCreationPanel')
    expect(source).not.toContain('MontageExecutionTarget')
  })

  it('plans page supports montage input', () => {
    const source = readFileSync('src/pages/PlansPage.tsx', 'utf8')
    expect(source).toContain('montage_input')
    expect(source).toContain('MontageCreationPanel')
  })
})
