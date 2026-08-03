import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('TasksPage recovery workspace contract', () => {
  it('keeps recovery signals compact and removes the dashboard-style workbench', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).not.toContain('恢复工作台')
    expect(source).toContain('需要处理')
    expect(source).toContain('个失败任务')
    expect(source).toContain('待发布确认')
    expect(source).toContain("to=\"/tasks?status=failed\"")
  })

  it('shows bulk controls only after tasks are selected', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('{selectedTaskIds.length > 0 && (')
    expect(source).toContain('清空选择')
    expect(source).toContain('下载选中文件')
  })

  it('explains why bulk actions skip selected tasks', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('可克隆')
    expect(source).toContain('其余将跳过')
    expect(source).toContain('可删除')
  })

  it('uses a project-aware centered dialog for task creation', () => {
    const pageSource = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')
    const dialogSource = readFileSync(join(here, '../components/tasks/TaskFormDialog.tsx'), 'utf8')

    expect(pageSource).toContain('<TaskFormDialog')
    expect(pageSource).toContain('initialProjectId={createDialogIntent.projectId}')
    expect(pageSource).toContain('onCreated={(task) => navigate(`/tasks/${task.id}`)}')
    expect(dialogSource).toContain('DialogContent')
    expect(dialogSource).not.toContain('SheetContent')
    expect(dialogSource).toContain('ProjectContextControl')
    expect(dialogSource).toContain('目标/提示词')
    expect(dialogSource).toContain('图片/高级')
    expect(dialogSource).toContain('固定价格')
    expect(dialogSource).toContain('createTaskFormDefaults')
    expect(dialogSource).toContain('switchTaskFormDefaults')
    expect(dialogSource).toContain('taskCreationCostPreview')
    expect(dialogSource).toContain('creationBlocker')
    expect(dialogSource).toContain('TaskComposerParameters')
    expect(dialogSource).not.toContain('ExecutionProfileToolbar')
    expect(dialogSource).not.toContain('ComposerQuantityControl')
    expect(dialogSource).toContain('leadingTools=')
    expect(dialogSource).not.toContain('ImageAspectRatioField')
    expect(dialogSource).not.toContain('01 类型')
  })

  it('keeps ecommerce creation on the base-fee path even if goal mode state is stale', () => {
    const dialogSource = readFileSync(join(here, '../components/tasks/TaskFormDialog.tsx'), 'utf8')
    const formSource = readFileSync(join(here, '../lib/task-form.ts'), 'utf8')

    expect(formSource).toContain("values.type !== 'ecommerce'")
    expect(formSource).toContain('goal_mode: values.goal_mode')
    expect(dialogSource).toContain('taskCreationCostPreview')
    expect(dialogSource).toContain("watchedType === 'ecommerce'")
    expect(dialogSource).toContain('ecommerceModuleCatalog')
  })

  it('keeps task rows focused on the primary business action', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')
    const helperSource = readFileSync(join(here, '../lib/studio-ux.ts'), 'utf8')

    expect(source).toContain('taskActionSignal')
    expect(helperSource).toContain('处理发布审批')
    expect(helperSource).toContain('查看失败原因')
    expect(helperSource).toContain('可下载、发布或复用')
  })
})
