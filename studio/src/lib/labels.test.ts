import { describe, it, expect } from 'vitest'
import { contentTypeLabel, contentTypeOptions, platformDefaultRatio, platformLabels, progressStageLabel, statusBadgeVariant, taskFailurePresentation, taskTypeLabelCN } from './labels'

describe('statusBadgeVariant', () => {
  it('returns "outline" for running', () => {
    expect(statusBadgeVariant('running')).toBe('outline')
  })

  it('returns "secondary" for completed', () => {
    expect(statusBadgeVariant('completed')).toBe('secondary')
  })

  it('returns "destructive" for failed', () => {
    expect(statusBadgeVariant('failed')).toBe('destructive')
  })

  it('returns "secondary" for pending', () => {
    expect(statusBadgeVariant('pending')).toBe('secondary')
  })

  it('returns "secondary" for cancelled', () => {
    expect(statusBadgeVariant('cancelled')).toBe('secondary')
  })

  it('returns "secondary" for unknown status', () => {
    expect(statusBadgeVariant('unknown')).toBe('secondary')
  })
})

describe('moments labels', () => {
  it('uses 朋友圈 labels and 3:4 default ratio', () => {
    expect(taskTypeLabelCN.moments).toBe('朋友圈')
    expect(contentTypeLabel.moments).toBe('朋友圈')
    expect(platformLabels.moments).toBe('朋友圈')
    expect(platformDefaultRatio.moments).toBe('3:4')
  })

  it('labels moments-specific progress stages', () => {
    expect(progressStageLabel.material_analysis).toBe('素材分析')
    expect(progressStageLabel.quality_review).toBe('质量复核')
  })
})

describe('taskFailurePresentation', () => {
  it('localizes execution identity failures and keeps the code', () => {
    expect(taskFailurePresentation({ error_message: '{"error_code":"execution_identity_unavailable"}' })).toMatchObject({
      code: 'execution_identity_unavailable',
      title: '执行环境未建立',
      recovery: '修复执行环境后可从“图片生成”阶段继续。',
    })
  })

  it('maps a known public error without reading internal execution evidence', () => {
    expect(taskFailurePresentation({
      error_message: '执行环境未建立，暂时无法生成或结算图片',
    })).toMatchObject({
      code: 'execution_identity_unavailable',
      title: '执行环境未建立',
      message: '执行环境未建立，暂时无法生成或结算图片。',
      raw: '执行环境未建立，暂时无法生成或结算图片',
    })
  })

  it('maps completion report failures to a recoverable result message', () => {
    expect(taskFailurePresentation({ error_message: 'completion_report_failed' })?.title).toBe('结果提交失败（可恢复）')
  })
})


describe('video platform labels', () => {
  it.each([
    ['montage', '视频生成'],
    ['hypit', '视频复刻'],
  ])('uses one name for %s in cards, timelines and type selectors', (type, label) => {
    expect(taskTypeLabelCN[type]).toBe(label)
    expect(contentTypeLabel[type]).toBe(label)
    expect(platformLabels[type]).toBe(label)
    expect(contentTypeOptions.find(option => option.value === type)?.label).toBe(label)
  })
})
