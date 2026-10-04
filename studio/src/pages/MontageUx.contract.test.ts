import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('montage UX contracts', () => {
  it('tasks page uses dedicated montage input and no user execution target selector', () => {
    const pageSource = readFileSync('src/pages/TasksPage.tsx', 'utf8')
    const dialogSource = readFileSync('src/components/tasks/TaskFormDialog.tsx', 'utf8')
    const parametersSource = readFileSync('src/components/tasks/TaskComposerParameters.tsx', 'utf8')
    expect(pageSource).toContain('TaskFormDialog')
    expect(dialogSource).toContain('montage_input')
    expect(dialogSource).toContain('MontageCreationPanel')
    expect(dialogSource).toContain('TaskComposerParameters')
    expect(parametersSource).toContain('ExecutionProfileSelector')
    expect(dialogSource).not.toContain('runThisTaskLocally')
    expect(dialogSource).not.toContain('execution_target')
    expect(dialogSource).not.toContain('MontageExecutionTarget')
  })

  it('plans keep output selection independent from task-specific inputs', () => {
    const source = readFileSync('src/pages/PlansPage.tsx', 'utf8')
    expect(source).toContain('agent_ids')
    expect(source).not.toContain('montage_input')
    expect(source).not.toContain('MontageCreationPanel')
  })
})
