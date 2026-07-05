import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

function pageSource(name: string) {
  return readFileSync(join(here, name), 'utf8')
}

describe('video generation UX contracts', () => {
  it('task creation uses server video estimate and reference assets', () => {
    const source = pageSource('TasksPage.tsx')

    expect(source).toContain('VideoCreationPanel')
    expect(source).toContain('VideoEstimateSummary')
    expect(source).toContain('api.video.estimate')
    expect(source).not.toContain('model.display_name || model.key')
    expect(source).toContain('视频任务需至少')
  })

  it('plan creation uses server video estimate and reference assets', () => {
    const source = pageSource('PlansPage.tsx')

    expect(source).toContain('VideoCreationPanel')
    expect(source).toContain('VideoEstimateSummary')
    expect(source).toContain('api.video.estimate')
    expect(source).not.toContain('model.display_name || model.key')
    expect(source).toContain('视频任务需至少')
  })

  it('task detail shows video results through generated file preview', () => {
    const source = pageSource('TaskDetailPage.tsx')
    const previewSource = readFileSync(join(here, '../components/FilePreview.tsx'), 'utf8')

    expect(source).toContain('renderPreviewDetails')
    expect(source).toContain('输入与创作参数')
    expect(source).toContain('creative_type')
    expect(source).toContain('subject_profile')
    expect(source).toContain('参考素材')
    expect(source).toContain('生成任务 ID')
    expect(source).toContain('费用明细')
    expect(source).not.toContain('const videoFiles =')
    expect(source).not.toContain('Video files')
    expect(source).toContain('nonImageFiles')
    expect(source).toContain('files={nonImageFiles}')
    expect(source).not.toMatch(/files=\{nonImageFiles\}[\s\S]{0,220}inlineItemClassName/)
    expect(previewSource).toContain('视频结果')
    expect(previewSource).toContain('<video')
    expect(previewSource).toContain('filePreviewTone')
    expect(previewSource).toContain('border-sky')
    expect(previewSource).toContain('border-emerald')
    expect(previewSource).toContain('border-amber')
    expect(previewSource).toContain('border-violet')
    expect(previewSource).toContain('sm:grid-cols-[minmax(0,1fr)_320px]')
  })
})
