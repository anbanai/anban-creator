import { describe, expect, it } from 'vitest'
import type { TaskFile, TaskLifecycle } from '@/types'
import { groupTaskStageFiles } from './task-stage-files'

const lifecycle: TaskLifecycle = {
  version: 1, revision: 1, execution_id: 'current', updated_at: '',
  stages: [
    { id: 'research', title: '选题研究', source: 'agent', kind: 'work', state: 'complete' },
    { id: 'writing', title: '正文创作', source: 'agent', kind: 'work', state: 'complete' },
    { id: 'visuals', title: '封面与配图', source: 'agent', kind: 'work', state: 'failed' },
    { id: 'draft', title: '公众号草稿', source: 'server', kind: 'draft', state: 'pending' },
  ],
}
function file(id: string, overrides: Partial<TaskFile> = {}): TaskFile {
  return { id, task_id: 'task', execution_id: 'current', state: 'delivered', role: 'other', file_name: id, mime_type: 'application/json', file_size: 100, url: '', created_at: '', ...overrides }
}

describe('groupTaskStageFiles', () => {
  it.each(['main_image', 'detail_image', 'cover_image', 'share_image', 'sku_image', 'tail'])('falls back to the stored image role for the %s delivery role', (deliveryRole) => {
    const result = groupTaskStageFiles([file('main_01.png', { role: 'image', delivery_role: deliveryRole })], lifecycle)
    expect(result.byStage.get('visuals')?.map((item) => item.id)).toEqual(['main_01.png'])
    expect(result.unassigned).toEqual([])
  })

  it('uses declared artifact paths before role inference and assigns every file exactly once', () => {
    const files = [file('findings.md', { role: 'markdown' }), file('article.md', { role: 'final_markdown' }), file('cover.png', { role: 'cover' }), file('unknown.json')]
    const result = groupTaskStageFiles(files, lifecycle, {
      version: 'creation_workflow_v1', current_stage: 'writing',
      stages: [{ key: 'research', label: '选题研究', status: 'completed', artifact_paths: ['output/findings.md'] }],
    })
    expect(result.byStage.get('research')?.map((item) => item.id)).toEqual(['findings.md'])
    expect(result.byStage.get('writing')?.map((item) => item.id)).toEqual(['article.md'])
    expect(result.byStage.get('visuals')?.map((item) => item.id)).toEqual(['cover.png'])
    expect(result.unassigned.map((item) => item.id)).toEqual(['unknown.json'])
  })

  it('uses explicit producer wording to keep topic analysis in the research stage', () => {
    const files = [file('topic-analysis.md', { role: 'markdown', delivery_role: 'content' })]
    const plan: TaskLifecycle = {
      ...lifecycle,
      stages: [
        { ...lifecycle.stages[0], title: '选题研究与遴选', latest_update: '基于素材研究选题，评分选出 Top1 并写 topic-analysis.md' },
        { ...lifecycle.stages[1], title: '内容创作与标题锁定' },
      ],
    }
    const result = groupTaskStageFiles(files, plan)

    expect(result.byStage.get('research')).toEqual(files)
    expect(result.byStage.has('writing')).toBe(false)
    expect(result.unassigned).toEqual([])
  })

  it('uses the server producer stage id before file-role inference', () => {
    const files = [file('topic-analysis.md', { role: 'markdown', delivery_role: 'content', producer_stage_id: 'research' })]
    const result = groupTaskStageFiles(files, lifecycle)

    expect(result.byStage.get('research')).toEqual(files)
    expect(result.byStage.has('writing')).toBe(false)
  })

  it('leaves a file unassigned when multiple stages explicitly claim to produce it', () => {
    const files = [file('topic-analysis.md', { role: 'markdown', delivery_role: 'content' })]
    const plan: TaskLifecycle = {
      ...lifecycle,
      stages: [
        { ...lifecycle.stages[0], goal: '研究选题并写 topic-analysis.md' },
        { ...lifecycle.stages[1], goal: '再生成 topic-analysis.md' },
      ],
    }
    const result = groupTaskStageFiles(files, plan)

    expect(result.byStage.size).toBe(0)
    expect(result.unassigned).toEqual(files)
  })

  it('keeps the producer stable when a later review mentions the same file', () => {
    const files = [file('04-article-final.md', { role: 'markdown', delivery_role: 'final_markdown' })]
    const plan: TaskLifecycle = { ...lifecycle, stages: [lifecycle.stages[1], { ...lifecycle.stages[1], id: 'review', title: '质量复核', latest_update: '内容核验完成' }] }
    expect(groupTaskStageFiles(files, plan).byStage.get('writing')).toEqual(files)
    plan.stages[1].latest_update = '已复核 04-article-final.md，事实无误'
    expect(groupTaskStageFiles(files, plan).byStage.get('writing')).toEqual(files)
  })

  it('keeps previous executions separate from current stages', () => {
    const result = groupTaskStageFiles([file('old.md', { role: 'final_markdown', execution_id: 'previous' })], lifecycle)
    expect(result.byStage.size).toBe(0)
    expect(result.historical.map((item) => item.id)).toEqual(['old.md'])
  })

  it('does not guess between ambiguous stages or assign files to skipped or server steps', () => {
    const result = groupTaskStageFiles([file('cover.png', { role: 'cover' }), file('unknown.json')], {
      ...lifecycle,
      stages: [
        { ...lifecycle.stages[2], id: 'visuals-a', state: 'complete' },
        { ...lifecycle.stages[2], id: 'visuals-b', state: 'complete' },
        { ...lifecycle.stages[1], state: 'skipped', goal: 'unknown.json' },
        { ...lifecycle.stages[3], goal: 'unknown.json', state: 'complete' },
      ],
    })
    expect(result.byStage.size).toBe(0)
    expect(result.unassigned.map((item) => item.id)).toEqual(['cover.png', 'unknown.json'])
  })

  it('matches named video generation outputs to the producing pipeline instead of skipped delivery registration', () => {
    const result = groupTaskStageFiles([file('final.mp4', { role: 'video' }), file('montage-project.json')], {
      ...lifecycle,
      stages: [
        { ...lifecycle.stages[0], id: 'preflight', title: '能力预检与生产决策' },
        { ...lifecycle.stages[1], id: 'pipeline', title: '执行上游 OpenMontage 管线', goal: '生成 final.mp4 和 montage-project.json', state: 'failed' },
        { ...lifecycle.stages[2], id: 'delivery', title: '封面与交付登记', state: 'skipped' },
      ],
    })
    expect(result.byStage.get('pipeline')?.map((item) => item.id)).toEqual(['final.mp4', 'montage-project.json'])
    expect(result.unassigned).toEqual([])
  })

  it('keeps files available with no plan, and uses a single declared work step when present', () => {
    const files = [file('notes.md')]
    expect(groupTaskStageFiles(files).unassigned).toEqual(files)
    expect(groupTaskStageFiles(files, { ...lifecycle, stages: [lifecycle.stages[0]] }).byStage.get('research')).toEqual(files)
  })

  it('does not attach unknown files to the only started step of a larger plan', () => {
    const files = [file('unknown.json')]
    const result = groupTaskStageFiles(files, { ...lifecycle, stages: lifecycle.stages.map((stage, index) => ({ ...stage, state: index === 0 ? 'failed' : 'skipped' })) })
    expect(result.byStage.size).toBe(0)
    expect(result.unassigned).toEqual(files)
  })

  it('recognizes the video generation project when its generic file role carries no stage information', () => {
    const result = groupTaskStageFiles([file('montage-project.json')], { ...lifecycle, stages: [
      { ...lifecycle.stages[0], title: '能力预检与生产决策' },
      { ...lifecycle.stages[1], id: 'pipeline', title: '执行上游 OpenMontage 管线', state: 'failed' },
      { ...lifecycle.stages[2], title: '封面与交付登记', state: 'skipped' },
    ] })
    expect(result.byStage.get('pipeline')?.map((item) => item.id)).toEqual(['montage-project.json'])
  })
})
