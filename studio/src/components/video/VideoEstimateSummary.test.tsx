import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { VideoEstimateSummary } from './VideoEstimateSummary'
import type { VideoEstimateResponse } from '@/types'

describe('VideoEstimateSummary', () => {
  it('surfaces production guidance from estimate', () => {
    const estimate: VideoEstimateResponse = {
      available_models: [],
      resolved_config: { model_key: 'seedance-2.0-mini', resolution: '720p', ratio: '9:16', duration: 16 },
      estimated_credits: 8000,
      balance: 120000,
      min_balance: 0,
      meets_min_balance: true,
      missing_reference_roles: ['action', 'voice tone'],
      expected_artifacts: ['creative-brief.md', 'quality-review.md'],
      affordable_takes: 5,
      segment_plan: [
        { index: 1, start_second: 0, end_second: 8, duration: 8 },
        { index: 2, start_second: 8, end_second: 16, duration: 8 },
      ],
    }

    render(<VideoEstimateSummary estimate={estimate} />)

    expect(screen.getByText('后续视频生成操作费')).toBeInTheDocument()
    expect(screen.getByText(/MCP video_gen 提交时扣除/)).toBeInTheDocument()
    expect(screen.queryByText(/创建门槛/)).not.toBeInTheDocument()
    expect(screen.getByText('缺少参考角色')).toBeInTheDocument()
    expect(screen.getByText('action、voice tone')).toBeInTheDocument()
    expect(screen.getByText('2 段生成计划')).toBeInTheDocument()
    expect(screen.getByText('预算内约 5 次 take')).toBeInTheDocument()
    expect(screen.getByText('creative-brief.md、quality-review.md')).toBeInTheDocument()
  })
})
