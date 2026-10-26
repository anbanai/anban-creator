import { describe, it, expect } from 'vitest'
import { cronToHuman, contentTypeDisplayName, contentTypeLabel, contentTypeOptions, formatDateTimeCN, platformDefaultRatio, platformDisplayName, platformLabels, progressStageLabel, statusBadgeVariant, taskFailurePresentation, taskTypeLabelCN } from './labels'

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

  it('shows the server-owned Catalog mismatch diagnostic instead of generic runtime_failed', () => {
    expect(taskFailurePresentation({
      error_message: 'runtime_failed',
      outcome: {
        core_delivery: { status: 'none' },
        visual: { status: 'not_requested' },
        review: { status: 'unavailable' },
        publication: { status: 'not_requested' },
        warnings: [],
        diagnostic: {
          code: 'agent_pack_catalog_mismatch',
          recoverable: true,
          summary: 'Agent 运行镜像与 Server 的 Agent Pack Catalog 不一致，请重新构建并部署同一版本的 Server 与 Agent 镜像后重试。',
        },
      },
    })).toMatchObject({
      code: 'agent_pack_catalog_mismatch',
      title: '运行镜像版本不一致',
      recovery: '重新构建并部署 Server 与对应 Agent Pack 镜像，再创建新的任务。',
    })
  })
})


describe('video platform labels', () => {
  it.each([
    ['montage', '视频生成'],
    ['hypit', '视频复刻'],
    ['video-edit', '全自动视频剪辑'],
  ])('uses one name for %s in cards, timelines and type selectors', (type, label) => {
    expect(taskTypeLabelCN[type]).toBe(label)
    expect(contentTypeLabel[type]).toBe(label)
    expect(platformLabels[type]).toBe(label)
    expect(contentTypeOptions.find(option => option.value === type)?.label).toBe(label)
  })

  it('resolves every known platform to a display name without leaking raw keys', () => {
    expect(platformDisplayName('montage')).toBe('视频生成')
    expect(platformDisplayName('hypit')).toBe('视频复刻')
    expect(platformDisplayName('video-edit')).toBe('全自动视频剪辑')
    expect(platformDisplayName('viral-analysis')).toBe('爆文拆解')
    expect(platformDisplayName(undefined)).toBe('通用')
    expect(platformDisplayName('', '上下文')).toBe('上下文')
    expect(platformDisplayName('unknown_platform')).toBe('unknown_platform')
  })

  it('keeps content type wording distinct from platform wording', () => {
    expect(contentTypeDisplayName('wechat-article')).toBe('公众号文章')
    expect(platformDisplayName('wechat')).toBe('公众号')
    expect(contentTypeDisplayName('montage')).toBe('视频生成')
    expect(contentTypeDisplayName('hypit')).toBe('视频复刻')
    expect(contentTypeDisplayName('video-edit')).toBe('全自动视频剪辑')
    expect(contentTypeDisplayName('viral-analysis')).toBe('爆文拆解')
    expect(contentTypeDisplayName(null)).toBe('通用')
    expect(contentTypeDisplayName('unknown_type')).toBe('unknown_type')
  })
})

describe('plan schedule summaries', () => {
  it.each([
    ['0 9 * * 1,3,5', '每周一、三、五 09:00'],
    ['7 12 * * *', '每天 12:07'],
    ['0 9 * * 0', '每周日 09:00'],
    ['0 9 1 * *', '0 9 1 * *'],
    ['*/15 * * * *', '*/15 * * * *'],
  ])('describes the complete schedule %s', (cron, expected) => {
    expect(cronToHuman(cron)).toBe(expected)
  })
})

describe('date-time labels', () => {
  it('includes the year for current-year timestamps too', () => {
    expect(formatDateTimeCN('2026-01-01T00:05:00Z')).toContain('2026')
  })
})
