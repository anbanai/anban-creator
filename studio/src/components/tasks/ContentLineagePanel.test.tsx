import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import ContentLineagePanel from './ContentLineagePanel'

describe('ContentLineagePanel', () => {
  it('shows the asset, linked content, cost and recovery point from Server facts', async () => {
    vi.spyOn(api.contentAssets, 'getLineage').mockResolvedValue({
      asset: { id: 'asset-1', user_id: 'owner', title: 'AI 内容工作流', status: 'validated', lifecycle: 'evergreen', created_at: '', updated_at: '' },
      evidence: [], scores: [],
      links: [{ id: 'link-1', asset_id: 'asset-1', content_kind: 'task', content_id: 'source-1', platform: 'wechat', relation: 'origin', angle: '实操指南', created_at: '' }], summary: { origin_count: 1, derivative_count: 0, run_count: 0, published_count: 0, cost_credits: 0, reserved_credits: 0 }, platform_performance: [], latest_runs: [],
    })
    vi.spyOn(api.contentAssets, 'getRun').mockResolvedValue({ id: 'run-1', user_id: 'owner', asset_id: 'asset-1', target_project_id: 'target', target_platform: 'montage', stage: 'publish', status: 'paused', idempotency_key: 'key', budget_reserved: 0, cost_credits: 120, retry_count: 0, error_message: '等待平台发布能力', created_at: '', updated_at: '' })
    render(<ContentLineagePanel assetID="asset-1" runID="run-1" taskID="task-1" />)
    expect(await screen.findByRole('link', { name: 'AI 内容工作流' })).toHaveAttribute('href', '/content-assets?asset_id=asset-1')
    expect(await screen.findByText('等待平台发布能力')).toBeInTheDocument()
    expect(screen.getByText('来源 · 公众号')).toBeInTheDocument()
    expect(screen.getByText('120 积分')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /实操指南/ })).toHaveAttribute('href', '/tasks/source-1')
    expect(screen.getByRole('link', { name: '跨平台复用' })).toHaveAttribute('href', '/content-assets?reuse_asset_id=asset-1&source_task_id=task-1')
  })

  it('resumes a paused run through the Server and refreshes task evidence', async () => {
    vi.spyOn(api.contentAssets, 'getLineage').mockResolvedValue({ asset: { id: 'asset-1', user_id: 'owner', title: '选题', status: 'validated', lifecycle: 'evergreen', created_at: '', updated_at: '' }, evidence: [], scores: [], links: [], summary: { origin_count: 0, derivative_count: 0, run_count: 0, published_count: 0, cost_credits: 0, reserved_credits: 0 }, platform_performance: [], latest_runs: [] })
    const paused = { id: 'run-1', user_id: 'owner', asset_id: 'asset-1', target_project_id: 'target', target_platform: 'wechat', stage: 'publish', status: 'paused', idempotency_key: 'key', budget_reserved: 0, cost_credits: 100, retry_count: 0, created_at: '', updated_at: '' }
    vi.spyOn(api.contentAssets, 'getRun').mockResolvedValue(paused)
    const resume = vi.spyOn(api.contentAssets, 'setRunStatus').mockResolvedValue({ ...paused, status: 'queued' })
    render(<ContentLineagePanel assetID="asset-1" runID="run-1" taskID="task-1" />)
    fireEvent.click(await screen.findByRole('button', { name: '恢复运行' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith('run-1', 'queued'))
  })
})
