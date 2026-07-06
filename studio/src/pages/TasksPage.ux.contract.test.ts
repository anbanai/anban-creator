import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('TasksPage recovery workspace contract', () => {
  it('renders a recovery queue before the task list', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('恢复工作台')
    expect(source).toContain('失败待恢复')
    expect(source).toContain('待发布确认')
    expect(source).toContain('最近完成')
    expect(source).toContain("to=\"/settings\"")
    expect(source).toContain("to=\"/tasks?status=failed\"")
  })

  it('explains why bulk actions skip selected tasks', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('可克隆')
    expect(source).toContain('其余将跳过')
    expect(source).toContain('可删除')
  })
})
