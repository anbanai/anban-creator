import { describe, it, expect } from 'vitest'
import { contentTypeLabel, platformDefaultRatio, platformLabels, progressStageLabel, statusBadgeVariant, taskFailurePresentation, taskTypeLabelCN } from './labels'

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

  it('uses the structured terminal result when error_message is user-facing text', () => {
    expect(taskFailurePresentation({
      error_message: '执行环境未建立，暂时无法生成或结算图片',
      result: JSON.stringify({
        success: false,
        root_error_code: 'execution_identity_unavailable',
        failure_stage: 'image_generation',
        resume_from: 'image_generation',
      }),
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
