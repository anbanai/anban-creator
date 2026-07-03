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

    expect(source).toContain('VideoReferenceInput')
    expect(source).toContain('VideoEstimateSummary')
    expect(source).toContain('api.video.estimate')
    expect(source).not.toContain('<SelectItem value="seedance-2.0"')
    expect(source).toContain('视频任务需至少')
  })

  it('plan creation uses server video estimate and reference assets', () => {
    const source = pageSource('PlansPage.tsx')

    expect(source).toContain('VideoReferenceInput')
    expect(source).toContain('VideoEstimateSummary')
    expect(source).toContain('api.video.estimate')
    expect(source).not.toContain('<SelectItem value="seedance-2.0"')
    expect(source).toContain('视频任务需至少')
  })

  it('task detail has a video-specific result preview section', () => {
    const source = pageSource('TaskDetailPage.tsx')

    expect(source).toContain('视频结果')
    expect(source).toContain('<video')
    expect(source).toContain('参考素材')
    expect(source).toContain('生成任务 ID')
    expect(source).toContain('费用明细')
  })
})
