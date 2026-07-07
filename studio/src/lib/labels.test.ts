import { describe, it, expect } from 'vitest'
import { contentTypeLabel, platformDefaultRatio, platformLabels, progressStageLabel, statusBadgeVariant, taskTypeLabelCN } from './labels'

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
