import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('openmontage UX contracts', () => {
  it('tasks page uses dedicated openmontage input and no user execution target selector', () => {
    const source = readFileSync('src/pages/TasksPage.tsx', 'utf8')
    expect(source).toContain('openmontage_input')
    expect(source).toContain('OpenMontageCreationPanel')
    expect(source).not.toContain('OpenMontageExecutionTarget')
  })

  it('plans page supports openmontage input', () => {
    const source = readFileSync('src/pages/PlansPage.tsx', 'utf8')
    expect(source).toContain('openmontage_input')
    expect(source).toContain('OpenMontageCreationPanel')
  })
})
