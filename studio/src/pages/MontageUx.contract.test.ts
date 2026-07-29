import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('montage UX contracts', () => {
  it('tasks page uses dedicated montage input and no user execution target selector', () => {
    const pageSource = readFileSync('src/pages/TasksPage.tsx', 'utf8')
    const dialogSource = readFileSync('src/components/tasks/TaskFormDialog.tsx', 'utf8')
    expect(pageSource).toContain('TaskFormDialog')
    expect(dialogSource).toContain('montage_input')
    expect(dialogSource).toContain('MontageCreationPanel')
    expect(dialogSource).toContain('ExecutionProfileSelector')
    expect(dialogSource).not.toContain('runThisTaskLocally')
    expect(dialogSource).not.toContain('execution_target')
    expect(dialogSource).not.toContain('MontageExecutionTarget')
  })

  it('plans page supports montage input', () => {
    const source = readFileSync('src/pages/PlansPage.tsx', 'utf8')
    expect(source).toContain('montage_input')
    expect(source).toContain('MontageCreationPanel')
  })
})
