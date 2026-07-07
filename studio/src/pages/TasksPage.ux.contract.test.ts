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
    expect(source).toContain('放行到公众号草稿箱')
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

  it('uses a compact stepped sheet for task creation', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('SheetContent')
    expect(source).toContain('任务创建路径')
    expect(source).toContain('类型')
    expect(source).toContain('项目')
    expect(source).toContain('目标/提示词')
    expect(source).toContain('图片/高级')
    expect(source).toContain('基础任务费预估')
  })

  it('keeps ecommerce creation on the base-fee path even if goal mode state is stale', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain("goal_mode: values.type !== 'ecommerce' && goalMode ? true : undefined")
    expect(source).toContain('const multiplier = isEcom ? 1 : goalMode ? 3 : 1')
  })
})
