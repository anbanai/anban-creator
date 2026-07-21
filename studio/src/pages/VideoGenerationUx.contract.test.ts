import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

function pageSource(name: string) {
  return readFileSync(join(here, name), 'utf8')
}

describe('video generation UX contracts', () => {
  it('task creation uses intake contract without pre-generation estimates', () => {
    const source = pageSource('TasksPage.tsx')

    expect(source).toContain('VideoCreationPanel')
    expect(source).toContain('video_creator_input')
    expect(source).toContain('video_editor_input')
    expect(source).not.toContain('video_input')
    expect(source).not.toContain('VideoEstimateSummary')
    expect(source).not.toMatch(/api\.video(?!Creator)/)
    expect(source).not.toContain('视频风格与禁忌')
    for (const oldLabel of ['视频玩法', '制作模式', '工作流', '商业目标', '人物 / 主体', '目标受众', '核心信息']) {
      expect(source).not.toContain(oldLabel)
    }
  })

  it('plan creation uses intake contract without pre-generation estimates', () => {
    const source = pageSource('PlansPage.tsx')

    expect(source).toContain('VideoCreationPanel')
    expect(source).toContain('video_creator_input')
    expect(source).not.toContain('video_input')
    expect(source).not.toContain('video_editor_input')
    expect(source).not.toContain('VideoEstimateSummary')
    expect(source).not.toMatch(/api\.video(?!Creator)/)
    for (const oldLabel of ['视频玩法', '制作模式', '工作流', '商业目标', '人物 / 主体', '目标受众', '核心信息']) {
      expect(source).not.toContain(oldLabel)
    }
  })

  it('task detail shows video results through preview and audit configuration', () => {
    const source = pageSource('TaskDetailPage.tsx')
    const previewSource = readFileSync(join(here, '../components/FilePreview.tsx'), 'utf8')
    const detailsSource = readFileSync(join(here, '../components/tasks/TaskDetailsSheet.tsx'), 'utf8')
    const configurationSource = readFileSync(join(here, '../components/tasks/VideoTaskConfigurationDetails.tsx'), 'utf8')

    expect(source).toContain('renderPreviewDetails')
    expect(source).toContain('TaskDetailsSheet')
    expect(detailsSource).toContain('TaskConfigurationDetails')
    expect(configurationSource).toContain('用户输入')
    expect(configurationSource).toContain('Agent 解析结果')
    expect(source).toContain('creative_type')
    expect(source).toContain('subject_profile')
    expect(source).toContain('参考素材')
    expect(source).toContain('生成任务 ID')
    expect(source).toContain('任务固定价')
    expect(source).not.toContain('费用明细')
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
